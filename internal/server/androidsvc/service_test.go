package androidsvc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/compliance"
	"mdm/internal/server/mobile/android"
	"mdm/internal/server/mobile/android/fake"
	"mdm/internal/server/policy"
	"mdm/internal/server/secrets"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

type env struct {
	t     *testing.T
	s     *store.Store
	f     *fake.Server
	svc   *Service
	ent   string
	admin uuid.UUID
}

func setup(t *testing.T) *env {
	t.Helper()
	s := storetest.New(t)
	f := fake.New()
	t.Cleanup(f.Close)
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	box, _ := secrets.New(base64.StdEncoding.EncodeToString(k))
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	raw, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "p", "client_email": "e@p.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": f.TokenURL()})
	sealed, _ := box.Seal(raw)
	_ = s.SaveAndroidKey(ctx, sealed, "p", "e@p.iam.gserviceaccount.com", time.Now())
	ent := "enterprises/LC0001"
	f.Enterprises[ent] = map[string]any{"name": ent}
	_ = s.LinkAndroidEnterprise(ctx, ent, "Empresa", nil, time.Now())
	admin, _ := s.CreateAdmin(ctx, "ana@ejemplo.es", "x", "admin")
	return &env{t: t, s: s, f: f, ent: ent, admin: admin.ID,
		svc: New(Deps{Store: s, Secrets: box, APIBase: f.APIBase(), Now: time.Now, CheckinIntervalS: 60})}
}

func (e *env) device(id string, extra map[string]any) string {
	d := map[string]any{"state": "ACTIVE", "lastStatusReportTime": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"enrollmentTime":     time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
		"hardwareInfo":       map[string]any{"brand": "samsung", "manufacturer": "samsung", "model": "SM-A556B", "serialNumber": "R5CX" + id},
		"softwareInfo":       map[string]any{"androidVersion": "14", "securityPatchLevel": "2026-10-01", "androidBuildNumber": "UP1A"},
		"memoryInfo":         map[string]any{"totalRam": "8000000000", "totalInternalStorage": "128000000000"},
		"networkInfo":        map[string]any{"imei": "35000000000000" + id, "telephonyInfos": []any{map[string]any{"carrierName": "Movistar", "phoneNumber": "+34600000000"}}},
		"deviceSettings":     map[string]any{"isEncrypted": true, "isDeviceSecure": true},
		"securityPosture":    map[string]any{"devicePosture": "SECURE"},
		"applicationReports": []any{map[string]any{"packageName": "com.microsoft.teams", "displayName": "Microsoft Teams", "versionName": "1416/1.0.0", "state": "INSTALLED"}},
	}
	for k, v := range extra {
		d[k] = v
	}
	return e.f.AddDevice(e.ent, id, d)
}

func (e *env) sync() {
	e.t.Helper()
	if err := e.svc.Sync(ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) byAmapi(name string) store.Device {
	e.t.Helper()
	id, err := e.s.DeviceIDByAmapiName(ctx, name)
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	d, _ := e.s.DeviceByID(ctx, id)
	return d
}

func (e *env) inventory(id uuid.UUID) protocol.Inventory {
	raw, _, _ := e.s.DeviceInventory(ctx, id)
	var inv protocol.Inventory
	_ = json.Unmarshal(raw, &inv)
	return inv
}

func settingsPut(s *store.Store, admin uuid.UUID, mod func(*settings.Values)) error {
	v, err := settings.Load(ctx, s)
	if err != nil {
		return err
	}
	mod(&v)
	return settings.Save(ctx, s, v, admin, time.Now())
}

func (e *env) policyFor(ss ...policy.Setting) {
	e.t.Helper()
	p, err := e.s.CreatePolicy(ctx, store.NewPolicy{Name: "Móviles " + uuid.NewString()[:4], Platform: "android", Settings: ss, CreatedBy: e.admin})
	if err != nil {
		e.t.Fatal(err)
	}
	_ = e.s.SetAssignments(ctx, p.ID, []policy.Assignment{{}}, time.Now())
}

func camera() policy.Setting {
	return policy.Setting{Key: "android.camera", Mode: "enforce", Params: json.RawMessage(`{"disabled":true}`)}
}

func TestSync_CreatesDevicesWithInventoryAndGroup(t *testing.T) {
	e := setup(t)
	g, _ := e.s.CreateGroup(ctx, "Repartidores", "")
	tok, err := e.svc.CreateEnrollmentToken(ctx, TokenRequest{Name: "Furgonetas", GroupID: &g.ID, Duration: 24 * time.Hour, OneTime: false, By: e.admin})
	if err != nil || tok.QRCode == "" {
		t.Fatalf("%+v %v", tok, err)
	}
	name := e.device("1", map[string]any{"enrollmentTokenData": tok.ID.String()})
	e.sync()
	d := e.byAmapi(name)
	if d.Platform != "android" || d.GroupID == nil || *d.GroupID != g.ID || d.Model != "SM-A556B" || d.OSVersion != "14" || d.LastSeenAt == nil {
		t.Fatalf("equipo: %+v", d)
	}
	inv := e.inventory(d.ID)
	sw, _, _ := e.s.ListSoftware(ctx, d.ID, "", 1, 50)
	if inv.Mobile == nil || inv.Mobile.SecurityPatchLevel != "2026-10-01" || inv.Mobile.IMEI != "350000000000001" || inv.Mobile.Carrier != "Movistar" ||
		len(sw) != 1 || sw[0].Name != "Microsoft Teams" || inv.Security.DiskEncryption != "on" || inv.Hardware.MemoryBytes != 8000000000 {
		t.Fatalf("inventario: %+v %+v", inv.Mobile, inv)
	}
	e.sync()
	if n, _ := e.s.CountDevices(ctx); n != 1 {
		t.Fatalf("sincronizar dos veces no duplica: %d", n)
	}
}

func TestSync_PublishesPolicyOncePerCombination(t *testing.T) {
	e := setup(t)
	e.policyFor(camera())
	a, b := e.device("1", nil), e.device("2", nil)
	e.sync()
	want, _ := android.Translate([]protocol.PolicySetting{{Key: "android.camera", Mode: "enforce", Params: json.RawMessage(`{"disabled":true}`)}}, nil, nil)
	name := android.PolicyName(e.ent, want)
	if e.f.Devices[a]["policyName"] != name || e.f.Devices[b]["policyName"] != name || len(e.f.Policies) != 1 {
		t.Fatalf("política: %v %v %d", e.f.Devices[a]["policyName"], name, len(e.f.Policies))
	}
	if e.f.Policy(name)["cameraAccess"] != "CAMERA_ACCESS_DISABLED" {
		t.Fatal("contenido de la política")
	}
	before := len(e.f.Requests)
	e.sync()
	for _, r := range e.f.Requests[before:] {
		if strings.HasPrefix(r, "PATCH") {
			t.Fatalf("sin cambios no se vuelve a publicar ni a asignar: %s", r)
		}
	}
}

func TestSync_ComplianceFromNonComplianceDetails(t *testing.T) {
	e := setup(t)
	e.policyFor(camera())
	name := e.device("1", nil)
	e.sync() // publica y asigna
	pol := e.f.Devices[name]["policyName"]
	e.f.SetDevice(name, map[string]any{"appliedPolicyName": pol, "nonComplianceDetails": []any{map[string]any{"settingName": "cameraAccess", "nonComplianceReason": "USER_ACTION"}}})
	e.sync()
	id := e.byAmapi(name).ID
	_, _ = compliance.Recompute(ctx, e.s, id, time.Now())
	rec, rows, _ := e.s.Compliance(ctx, id)
	if rec.State != "noncompliant" || len(rows) != 1 || rows[0].Status != "noncompliant" || !strings.Contains(rows[0].Message, "USER_ACTION") {
		t.Fatalf("%+v %+v", rec, rows)
	}
	e.f.SetDevice(name, map[string]any{"nonComplianceDetails": []any{}})
	e.sync()
	_, _ = compliance.Recompute(ctx, e.s, id, time.Now())
	if rec, _, _ := e.s.Compliance(ctx, id); rec.State != "compliant" {
		t.Fatalf("%+v", rec)
	}
}

func TestSync_ServerRulesPatchAndIntegrity(t *testing.T) {
	e := setup(t)
	e.policyFor(camera())
	name := e.device("1", map[string]any{"securityPosture": map[string]any{"devicePosture": "POTENTIALLY_COMPROMISED"}})
	e.sync()
	pol := e.f.Devices[name]["policyName"]
	e.f.SetDevice(name, map[string]any{"appliedPolicyName": pol})
	e.sync()
	if err := settingsPut(e.s, e.admin, func(v *settings.Values) { v.MinAndroidPatchLevel = "2026-11-01" }); err != nil {
		t.Fatal(err)
	}
	id := e.byAmapi(name).ID
	_, _ = compliance.Recompute(ctx, e.s, id, time.Now())
	rec, _, _ := e.s.Compliance(ctx, id)
	joined := strings.Join(rec.Reasons, " | ")
	if rec.State != "noncompliant" || !strings.Contains(joined, "parche de seguridad") || !strings.Contains(joined, "integridad") {
		t.Fatalf("%+v", rec)
	}
}

func TestSync_DeletedInGoogleIsRevoked(t *testing.T) {
	e := setup(t)
	name := e.device("1", nil)
	e.sync()
	delete(e.f.Devices, name)
	e.sync()
	if d := e.byAmapi(name); d.RevokedAt == nil {
		t.Fatal("un dispositivo que ya no está en Google queda revocado")
	}
	// Un fallo de Google no revoca nada.
	other := e.device("2", nil)
	e.sync()
	e.f.FailNext("GET /v1/"+e.ent+"/devices", 503, 1)
	if err := e.svc.Sync(ctx); err == nil {
		t.Fatal("el fallo debe devolverse para reintentar")
	}
	if d := e.byAmapi(other); d.RevokedAt != nil {
		t.Fatal("un fallo al listar no puede revocar equipos")
	}
	cfg, _ := e.s.AndroidConfig(ctx)
	if cfg.LastSyncError == "" {
		t.Fatal("el error queda visible en la integración")
	}
}

func (e *env) batch(typ string, params string, dev uuid.UUID) uuid.UUID {
	e.t.Helper()
	b, err := e.s.CreateBatch(ctx, store.NewBatch{Type: typ, Params: json.RawMessage(params), CreatedBy: e.admin, CreatedByEmail: "ana@ejemplo.es",
		TargetDescription: "prueba", At: time.Now(), ExpiresIn: time.Hour}, []uuid.UUID{dev})
	if err != nil {
		e.t.Fatal(err)
	}
	cmds, _ := e.s.BatchCommands(ctx, b.ID)
	return cmds[0].ID
}

func TestCommands_LockAndOperationResult(t *testing.T) {
	e := setup(t)
	name := e.device("1", nil)
	e.sync()
	id := e.byAmapi(name).ID
	cmd := e.batch(protocol.CmdLock, `{}`, id)
	if err := e.svc.ProcessCommands(ctx); err != nil {
		t.Fatal(err)
	}
	c, _ := e.s.GetCommand(ctx, cmd)
	if c.Status != "sent" || len(e.f.Operations) != 1 {
		t.Fatalf("%+v", c)
	}
	for op := range e.f.Operations {
		e.f.CompleteOperation(op, "")
	}
	_ = e.svc.ProcessCommands(ctx)
	if c, _ := e.s.GetCommand(ctx, cmd); c.Status != "succeeded" {
		t.Fatalf("%+v", c)
	}
	failing := e.batch(protocol.CmdResetPassword, `{"new_password":"482913"}`, id)
	_ = e.svc.ProcessCommands(ctx)
	for op, o := range e.f.Operations {
		if o["done"] != true {
			e.f.CompleteOperation(op, "La contraseña no cumple la política.")
		}
	}
	_ = e.svc.ProcessCommands(ctx)
	if c, _ := e.s.GetCommand(ctx, failing); c.Status != "failed" || !strings.Contains(c.Error+c.Stderr, "no cumple") {
		t.Fatalf("%+v", c)
	}
}

func TestCommands_WipeDeletesAndRevokes(t *testing.T) {
	e := setup(t)
	name := e.device("1", nil)
	e.sync()
	id := e.byAmapi(name).ID
	cmd := e.batch(protocol.CmdWipe, `{"wipe_external_storage":true}`, id)
	_ = e.svc.ProcessCommands(ctx)
	c, _ := e.s.GetCommand(ctx, cmd)
	if c.Status != "succeeded" || len(e.f.Deleted) != 1 || !strings.Contains(e.f.Deleted[0], "WIPE_EXTERNAL_STORAGE") {
		t.Fatalf("%+v %v", c, e.f.Deleted)
	}
	if d, _ := e.s.DeviceByID(ctx, id); d.RevokedAt == nil {
		t.Fatal("borrado = revocado")
	}
}

func TestNotLinked_NoOp(t *testing.T) {
	e := setup(t)
	_ = e.s.UnlinkAndroidEnterprise(ctx)
	if err := e.svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.f.Requests) != 0 {
		t.Fatal("sin empresa vinculada no se llama a Google")
	}
}

func TestEnrollmentToken_GroupPolicyAndData(t *testing.T) {
	e := setup(t)
	e.policyFor(camera())
	tok, err := e.svc.CreateEnrollmentToken(ctx, TokenRequest{Name: "Almacén", Duration: 7 * 24 * time.Hour, OneTime: true, By: e.admin})
	if err != nil {
		t.Fatal(err)
	}
	sent := e.f.Tokens[tok.AmapiName]
	if sent["additionalData"] != tok.ID.String() || sent["oneTimeOnly"] != true || !strings.HasPrefix(sent["policyName"].(string), e.ent+"/policies/p-") {
		t.Fatalf("%+v", sent)
	}
	if len(e.f.Policies) != 1 {
		t.Fatal("el token lleva la política de partida ya publicada")
	}
	if err := e.svc.RevokeEnrollmentToken(ctx, tok.ID); err != nil || len(e.f.Tokens) != 0 {
		t.Fatalf("revocar: %v", err)
	}
}

func TestCommands_ResetPasswordSealed(t *testing.T) {
	e := setup(t)
	name := e.device("1", nil)
	e.sync()
	id := e.byAmapi(name).ID
	sealed, _ := e.svc.d.Secrets.Seal([]byte("482913"))
	e.batch(protocol.CmdResetPassword, `{"new_password":"`+sealed+`"}`, id)
	_ = e.svc.ProcessCommands(ctx)
	if len(e.f.Operations) != 1 {
		t.Fatal("la orden debe llegar a Google")
	}
	for _, r := range e.f.Requests {
		_ = r
	}
	if got := e.f.LastBody(); !strings.Contains(got, `"newPassword":"482913"`) {
		t.Fatalf("a Google llega la contraseña descifrada: %s", got)
	}
}
