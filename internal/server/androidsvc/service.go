// Package androidsvc coordina los móviles Android en el servidor: publica
// las políticas en Google (una por combinación distinta), crea tokens de
// inscripción, sincroniza dispositivos, inventario y cumplimiento, y envía
// las órdenes. Todo pasa por la Android Management API; no hay agente.
package androidsvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/mobile/android"
	"mdm/internal/server/policy"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

type Deps struct {
	Store            *store.Store
	Secrets          *secrets.Box
	APIBase          string
	Now              func() time.Time
	CheckinIntervalS int
	Log              *slog.Logger
}

type Service struct{ d Deps }

func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{d: d}
}

// ErrNotLinked indica que no hay ninguna empresa vinculada con Google.
var ErrNotLinked = errors.New("no hay ninguna empresa de Android vinculada")

func (s *Service) client(ctx context.Context) (*android.Client, store.AndroidConfig, error) {
	cfg, err := s.d.Store.AndroidConfig(ctx)
	if errors.Is(err, store.ErrNotFound) || (err == nil && cfg.EnterpriseName == "") {
		return nil, cfg, ErrNotLinked
	}
	if err != nil {
		return nil, cfg, err
	}
	if s.d.Secrets == nil {
		return nil, cfg, errors.New("el servidor no tiene MDM_SECRET_KEY")
	}
	raw, err := s.d.Secrets.Open(cfg.SealedKey)
	if err != nil {
		return nil, cfg, fmt.Errorf("no se puede descifrar la clave de Google: %w", err)
	}
	sa, err := android.ParseServiceAccount(raw)
	if err != nil {
		return nil, cfg, err
	}
	return android.NewClient(sa, s.d.APIBase, nil), cfg, nil
}

func (s *Service) open(v string) (string, error) {
	if !strings.HasPrefix(v, "v1:") {
		return v, nil
	}
	b, err := s.d.Secrets.Open(v)
	return string(b), err
}

// publish devuelve el nombre de la política de Google para esa política
// efectiva, publicándola si es una combinación nueva.
func (s *Service) publish(ctx context.Context, c *android.Client, enterprise string, eff protocol.EffectivePolicy) (string, error) {
	p, err := android.Translate(eff.Settings, eff.Apps, s.open)
	if err != nil {
		return "", err
	}
	hash := android.HashPolicy(p)
	if name, err := s.d.Store.PublishedAndroidPolicy(ctx, hash); err == nil && strings.HasPrefix(name, enterprise+"/") {
		return name, nil
	}
	name := android.PolicyName(enterprise, p)
	if err := c.PatchPolicy(ctx, name, p); err != nil {
		return "", err
	}
	return name, s.d.Store.SaveAndroidPolicy(ctx, hash, name)
}

// groupPolicy es la política efectiva de un equipo nuevo del grupo (o sin grupo).
func (s *Service) groupPolicy(ctx context.Context, groupID *uuid.UUID) (protocol.EffectivePolicy, error) {
	ps, err := s.d.Store.AssignedPolicies(ctx, "android")
	if err != nil {
		return protocol.EffectivePolicy{}, err
	}
	var groups []uuid.UUID
	if groupID != nil {
		groups = []uuid.UUID{*groupID}
	}
	eff, _ := policy.Effective(ps, groups, "android", nil)
	deps, err := s.d.Store.AssignedDeployments(ctx, "android")
	if err != nil {
		return eff, err
	}
	eff.Apps, _ = policy.EffectiveApps(deps, groups, "android", nil)
	return eff, nil
}

type TokenRequest struct {
	Name     string
	GroupID  *uuid.UUID
	Duration time.Duration
	OneTime  bool
	By       uuid.UUID
}

// CreateEnrollmentToken crea un token de inscripción con la política de
// partida del grupo. additionalData lleva el id del token para saber, al
// sincronizar, en qué grupo poner el móvil.
func (s *Service) CreateEnrollmentToken(ctx context.Context, r TokenRequest) (store.AndroidToken, error) {
	c, cfg, err := s.client(ctx)
	if err != nil {
		return store.AndroidToken{}, err
	}
	eff, err := s.groupPolicy(ctx, r.GroupID)
	if err != nil {
		return store.AndroidToken{}, err
	}
	polName, err := s.publish(ctx, c, cfg.EnterpriseName, eff)
	if err != nil {
		return store.AndroidToken{}, err
	}
	id, err := s.d.Store.CreateAndroidToken(ctx, store.AndroidToken{Name: r.Name, GroupID: r.GroupID, OneTime: r.OneTime,
		ExpiresAt: s.d.Now().Add(r.Duration)}, r.By)
	if err != nil {
		return store.AndroidToken{}, err
	}
	tok, err := c.CreateEnrollmentToken(ctx, cfg.EnterpriseName, android.EnrollmentTokenRequest{PolicyName: polName, Duration: r.Duration,
		OneTimeOnly: r.OneTime, AdditionalData: id.String()})
	if err != nil {
		_ = s.d.Store.DeleteAndroidToken(ctx, id)
		return store.AndroidToken{}, err
	}
	if err := s.d.Store.SetAndroidTokenGoogle(ctx, id, tok.Name, tok.QRCode, tok.ExpiresAt); err != nil {
		return store.AndroidToken{}, err
	}
	return s.d.Store.AndroidToken(ctx, id)
}

func (s *Service) RevokeEnrollmentToken(ctx context.Context, id uuid.UUID) error {
	c, _, err := s.client(ctx)
	if err != nil {
		return err
	}
	t, err := s.d.Store.AndroidToken(ctx, id)
	if err != nil {
		return err
	}
	if t.AmapiName != "" {
		if err := c.DeleteEnrollmentToken(ctx, t.AmapiName); err != nil {
			var apiErr *android.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != 404 {
				return err
			}
		}
	}
	return s.d.Store.RevokeAndroidToken(ctx, id, s.d.Now().UTC())
}

func atoiU(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

func hostname(d android.Device) string {
	name := strings.TrimSpace(d.HardwareInfo.Model)
	if b := d.HardwareInfo.Brand; b != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(b)) {
		name = strings.ToUpper(b[:1]) + b[1:] + " " + name
	}
	if sn := d.HardwareInfo.SerialNumber; len(sn) >= 4 {
		name += " (" + sn[len(sn)-4:] + ")"
	}
	if name == "" {
		name = "Android " + d.Name[strings.LastIndex(d.Name, "/")+1:]
	}
	return name
}

func inventoryOf(d android.Device, policyApplied bool) protocol.Inventory {
	m := &protocol.Mobile{State: d.State, IMEI: d.NetworkInfo.IMEI, MEID: d.NetworkInfo.MEID, SecurityPatchLevel: d.SoftwareInfo.SecurityPatchLevel,
		BuildNumber: d.SoftwareInfo.AndroidBuildNumber, Integrity: d.SecurityPosture.DevicePosture, DeviceSecure: d.DeviceSettings.IsDeviceSecure,
		DeveloperOptions: d.DeviceSettings.DevelopmentSettingsEnabled, UnknownSources: d.DeviceSettings.UnknownSourcesEnabled,
		StorageBytes: atoiU(d.MemoryInfo.TotalInternalStorage), PolicyApplied: policyApplied}
	if !d.LastStatusReportTime.IsZero() {
		m.LastStatusReport = d.LastStatusReportTime.UTC().Format(time.RFC3339)
	}
	if len(d.NetworkInfo.TelephonyInfos) > 0 {
		m.Carrier, m.PhoneNumber = d.NetworkInfo.TelephonyInfos[0].CarrierName, d.NetworkInfo.TelephonyInfos[0].PhoneNumber
	}
	enc := "off"
	if d.DeviceSettings.IsEncrypted {
		enc = "on"
	}
	inv := protocol.Inventory{CollectedAt: d.LastStatusReportTime,
		System: protocol.System{Hostname: hostname(d), OSName: "Android", OSVersion: d.SoftwareInfo.AndroidVersion},
		Hardware: protocol.Hardware{Manufacturer: d.HardwareInfo.Manufacturer, Model: d.HardwareInfo.Model, Serial: d.HardwareInfo.SerialNumber,
			MemoryBytes: atoiU(d.MemoryInfo.TotalRAM)},
		Security: protocol.Security{DiskEncryption: enc}, Mobile: m, Disks: []protocol.Disk{}, Network: []protocol.NetIf{}, Software: []protocol.Software{}}
	if d.NetworkInfo.WifiMacAddress != "" {
		inv.Network = append(inv.Network, protocol.NetIf{Name: "wlan0", MAC: d.NetworkInfo.WifiMacAddress})
	}
	for _, a := range d.ApplicationReports {
		if a.State == "REMOVED" {
			continue
		}
		name := a.DisplayName
		if name == "" {
			name = a.PackageName
		}
		inv.Software = append(inv.Software, protocol.Software{Name: name, Version: a.VersionName, Publisher: a.PackageName})
	}
	b, _ := json.Marshal(inv)
	sum := sha256.Sum256(b)
	inv.Hash = "sha256:" + hex.EncodeToString(sum[:])
	return inv
}

// Sync lee todos los dispositivos de Google y actualiza los del MDM.
func (s *Service) Sync(ctx context.Context) error {
	c, cfg, err := s.client(ctx)
	if errors.Is(err, ErrNotLinked) {
		return nil
	}
	if err != nil {
		return err
	}
	now := s.d.Now().UTC()
	devs, err := c.ListDevices(ctx, cfg.EnterpriseName)
	if err != nil {
		_ = s.d.Store.SetAndroidSync(ctx, now, err.Error())
		return err
	}
	present := []string{}
	var firstErr error
	for _, d := range devs {
		if d.State == "DELETED" {
			continue
		}
		present = append(present, d.Name)
		if err := s.syncDevice(ctx, c, cfg.EnterpriseName, d, now); err != nil {
			s.d.Log.Warn("android: sincronizar dispositivo", "device", d.Name, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if _, err := s.d.Store.RevokeAndroidMissing(ctx, present, now); err != nil {
		return err
	}
	msg := ""
	if firstErr != nil {
		msg = firstErr.Error()
	}
	_ = s.d.Store.SetAndroidSync(ctx, now, msg)
	return firstErr
}

func (s *Service) syncDevice(ctx context.Context, c *android.Client, enterprise string, d android.Device, now time.Time) error {
	var group *uuid.UUID
	if tid, err := uuid.Parse(d.EnrollmentTokenData); err == nil {
		if t, err := s.d.Store.AndroidToken(ctx, tid); err == nil {
			group = t.GroupID
		}
	}
	var seen *time.Time
	if !d.LastStatusReportTime.IsZero() {
		t := d.LastStatusReportTime.UTC()
		seen = &t
	}
	enrolled := d.EnrollmentTime
	if enrolled.IsZero() {
		enrolled = now
	}
	id, _, err := s.d.Store.UpsertAndroidDevice(ctx, store.AndroidDevice{AmapiName: d.Name, Hostname: hostname(d), Serial: d.HardwareInfo.SerialNumber,
		Manufacturer: d.HardwareInfo.Manufacturer, Model: d.HardwareInfo.Model, OSVersion: d.SoftwareInfo.AndroidVersion, LastSeenAt: seen,
		EnrolledAt: enrolled, GroupID: group})
	if err != nil {
		return err
	}
	eff, _, err := s.d.Store.EffectivePolicyFor(ctx, id)
	if err != nil {
		return err
	}
	name, err := s.publish(ctx, c, enterprise, eff)
	if err != nil {
		return err
	}
	if d.PolicyName != name {
		if err := c.SetDevicePolicy(ctx, d.Name, name); err != nil {
			return err
		}
	}
	applied := d.AppliedPolicyName == name
	if err := s.d.Store.SaveInventory(ctx, id, inventoryOf(d, applied), now); err != nil {
		return err
	}
	if applied {
		if err := s.d.Store.SaveComplianceReport(ctx, id, complianceOf(d, eff), now); err != nil {
			return err
		}
		if err := s.d.Store.SaveAppStatus(ctx, id, appStatusOf(d, eff, now)); err != nil {
			return err
		}
	}
	return nil
}

// complianceOf traduce nonComplianceDetails a resultados por ajuste.
func complianceOf(d android.Device, eff protocol.EffectivePolicy) protocol.ComplianceReport {
	bad := map[string]string{}
	for _, n := range d.NonComplianceDetails {
		if k := android.SettingKey(n.SettingName, n.FieldPath); k != "" {
			bad[k] = "Android informa que no se cumple (" + n.NonComplianceReason + ")."
		}
	}
	rep := protocol.ComplianceReport{PolicyHash: eff.Hash, EvaluatedAt: d.LastStatusReportTime}
	for _, st := range eff.Settings {
		r := protocol.SettingResult{PolicyID: st.PolicyID, Key: st.Key, Status: protocol.StatusCompliant, Message: "Aplicado por Android."}
		if msg, ok := bad[st.Key]; ok {
			r.Status, r.Message = protocol.StatusNoncompliant, msg
		}
		rep.Results = append(rep.Results, r)
	}
	return rep
}

// appStatusOf deduce el estado de las aplicaciones de Google Play.
func appStatusOf(d android.Device, eff protocol.EffectivePolicy, now time.Time) []protocol.AppStatus {
	installed := map[string]bool{}
	for _, a := range d.ApplicationReports {
		if a.State != "REMOVED" {
			installed[a.PackageName] = true
		}
	}
	failed := map[string]string{}
	for _, n := range d.NonComplianceDetails {
		if n.PackageName != "" {
			failed[n.PackageName] = n.NonComplianceReason
		}
	}
	var out []protocol.AppStatus
	for _, a := range eff.Apps {
		if a.Source != "google_play" {
			continue
		}
		st := protocol.AppStatus{DeploymentID: a.DeploymentID, UpdatedAt: now}
		switch {
		case a.Intent == "uninstall" && !installed[a.PackageID]:
			st.State = protocol.AppAbsent
		case a.Intent == "uninstall":
			st.State = protocol.AppPending
		case installed[a.PackageID]:
			st.State = protocol.AppInstalled
		case failed[a.PackageID] != "":
			st.State, st.Message = protocol.AppFailed, "Google Play no la ha instalado ("+failed[a.PackageID]+")."
		case a.Intent == "available":
			st.State, st.Message = protocol.AppNotApplicable, "Disponible en la Play Store gestionada; el usuario decide si la instala."
		default:
			st.State = protocol.AppPending
		}
		out = append(out, st)
	}
	return out
}

// ProcessCommands envía a Google las órdenes pendientes de móviles y cierra
// las enviadas cuya operación ha terminado.
func (s *Service) ProcessCommands(ctx context.Context) error {
	c, _, err := s.client(ctx)
	if errors.Is(err, ErrNotLinked) {
		return nil
	}
	if err != nil {
		return err
	}
	now := s.d.Now().UTC()
	pending, err := s.d.Store.AndroidCommands(ctx, "pending", now)
	if err != nil {
		return err
	}
	for _, cmd := range pending {
		s.send(ctx, c, cmd, now)
	}
	sent, err := s.d.Store.AndroidCommands(ctx, "sent", now)
	if err != nil {
		return err
	}
	for _, cmd := range sent {
		if cmd.ExternalRef == "" {
			continue
		}
		op, err := c.GetOperation(ctx, cmd.ExternalRef)
		if err != nil || !op.Done {
			continue
		}
		_ = s.d.Store.FinishAndroidCommand(ctx, cmd.ID, op.Error == "", op.Error, now)
	}
	return nil
}

func (s *Service) send(ctx context.Context, c *android.Client, cmd store.AndroidCommand, now time.Time) {
	var p map[string]any
	_ = json.Unmarshal(cmd.Payload, &p)
	str := func(k string) string { v, _ := p[k].(string); return v }
	fail := func(msg string) { _ = s.d.Store.FinishAndroidCommand(ctx, cmd.ID, false, msg, now) }
	var ac android.Command
	switch cmd.Type {
	case protocol.CmdLock:
		ac.Type = "LOCK"
	case protocol.CmdReboot:
		ac.Type = "REBOOT"
	case protocol.CmdResetPassword:
		pass, err := s.open(str("new_password"))
		if err != nil {
			fail("No se puede descifrar la contraseña nueva (¿cambió MDM_SECRET_KEY?).")
			return
		}
		ac = android.Command{Type: "RESET_PASSWORD", NewPassword: pass, ResetPasswordFlags: []string{"LOCK_NOW", "REQUIRE_ENTRY"}}
	case protocol.CmdLostModeStart:
		ac = android.Command{Type: "START_LOST_MODE", StartLostModeParams: &android.LostMode{
			LostMessage: &android.UserFacing{DefaultMessage: str("message")}, LostPhoneNumber: &android.UserFacing{DefaultMessage: str("phone")}}}
	case protocol.CmdLostModeStop:
		ac.Type = "STOP_LOST_MODE"
	case protocol.CmdWipe:
		var flags []string
		if v, _ := p["wipe_external_storage"].(bool); v {
			flags = append(flags, "WIPE_EXTERNAL_STORAGE")
		}
		if err := c.DeleteDevice(ctx, cmd.AmapiName, flags); err != nil {
			fail("Google no ha aceptado el borrado: " + err.Error())
			return
		}
		_ = s.d.Store.FinishAndroidCommand(ctx, cmd.ID, true, "", now)
		_ = s.d.Store.RevokeDevice(ctx, cmd.DeviceID, now)
		return
	default:
		fail("Esta orden no existe en Android.")
		return
	}
	op, err := c.IssueCommand(ctx, cmd.AmapiName, ac)
	if err != nil {
		var apiErr *android.APIError
		if errors.As(err, &apiErr) && apiErr.Retryable() {
			return // se reintenta en la siguiente pasada
		}
		fail("Google no ha aceptado la orden: " + err.Error())
		return
	}
	if op.Done {
		_ = s.d.Store.FinishAndroidCommand(ctx, cmd.ID, op.Error == "", op.Error, now)
		return
	}
	_ = s.d.Store.MarkAndroidCommandSent(ctx, cmd.ID, op.Name, now)
}
