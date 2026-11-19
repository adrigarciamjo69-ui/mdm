package agentapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// cleanEnroll sanea las cadenas que envía el agente y recorta espacios.
func cleanEnroll(r *protocol.EnrollRequest) {
	for _, f := range []*string{&r.HardwareID, &r.Hostname, &r.Platform, &r.OSName, &r.OSVersion, &r.Arch,
		&r.Serial, &r.Manufacturer, &r.Model, &r.AgentVersion} {
		*f = strings.TrimSpace(store.CleanString(*f))
	}
}

func (a *api) enroll(w http.ResponseWriter, r *http.Request) {
	ip := a.ip(r)
	if blocked, wait := a.d.EnrollLimiter.Blocked(ip); blocked {
		tooManyRequests(w, wait)
		return
	}
	var req protocol.EnrollRequest
	if !httpx.DecodeJSON(w, r, maxSmallBody, &req) {
		return
	}
	cleanEnroll(&req)
	switch {
	case req.Token == "":
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Falta el token de inscripción.")
		return
	case req.HardwareID == "":
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Falta el identificador de hardware del equipo.")
		return
	case req.Hostname == "":
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Falta el nombre del equipo.")
		return
	case req.Platform != protocol.PlatformWindows && req.Platform != protocol.PlatformLinux:
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Plataforma no admitida: solo windows y linux.")
		return
	}

	secret, secretHash, err := auth.NewSecret()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	res, err := a.d.Store.EnrollDevice(r.Context(), store.EnrollParams{
		TokenHash: auth.HashSecret(req.Token), CredentialHash: secretHash, Req: req, IP: ip, At: a.d.Now().UTC(),
	})
	if errors.Is(err, store.ErrTokenInvalid) {
		// Solo los rechazos de token cuentan para el límite: las inscripciones
		// correctas de toda una oficina llegan desde la misma IP pública.
		a.d.EnrollLimiter.Record(ip)
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "El token de inscripción no es válido, ha caducado o ya no admite más equipos.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := store.EnqueueRecomputeDevice(r.Context(), a.d.Store.Pool(), res.DeviceID, a.d.Now()); err != nil {
		slog.Error("encolar recálculo de grupos", "device", res.DeviceID, "err", err)
	}
	action := audit.DeviceEnrolled
	details := map[string]any{"hostname": req.Hostname, "token_id": res.TokenID.String(), "token_name": res.TokenName}
	if res.Reenrolled {
		action = audit.DeviceReenroll
		// Un cambio de nombre en una reinscripción es la pista de dos equipos
		// clonados que comparten identificador de hardware.
		details["hostname_changed"] = res.PreviousHostname != "" && res.PreviousHostname != req.Hostname
		details["previous_hostname"] = res.PreviousHostname
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Action: action, TargetType: "device", TargetID: res.DeviceID.String(), Details: details, IP: ip})

	httpx.WriteJSON(w, http.StatusCreated, protocol.EnrollResponse{
		DeviceID:           res.DeviceID.String(),
		Credential:         secret,
		CheckinIntervalS:   protocol.DefaultCheckinIntervalS,
		InventoryIntervalS: protocol.DefaultInventoryIntervalS,
	})
}
