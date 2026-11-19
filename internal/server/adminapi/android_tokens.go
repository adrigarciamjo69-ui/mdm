package adminapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/server/androidsvc"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

func (a *api) androidSvc() *androidsvc.Service {
	return androidsvc.New(androidsvc.Deps{Store: a.d.Store, Secrets: a.d.Secrets, APIBase: a.d.AndroidAPIBase, Now: a.d.Now,
		CheckinIntervalS: a.d.CheckinIntervalS})
}

type androidTokenJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	QRCode    string     `json:"qr_code"`
	GroupID   *string    `json:"group_id"`
	GroupName string     `json:"group_name"`
	OneTime   bool       `json:"one_time"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func toAndroidTokenJSON(t store.AndroidToken) androidTokenJSON {
	out := androidTokenJSON{ID: t.ID.String(), Name: t.Name, QRCode: t.QRCode, GroupName: t.GroupName, OneTime: t.OneTime,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt}
	if t.GroupID != nil {
		s := t.GroupID.String()
		out.GroupID = &s
	}
	return out
}

func (a *api) listAndroidTokens(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Store.AndroidTokens(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]androidTokenJSON, 0, len(list))
	for _, t := range list {
		out = append(out, toAndroidTokenJSON(t))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createAndroidToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string  `json:"name"`
		GroupID      *string `json:"group_id"`
		DurationDays int     `json:"duration_days"`
		OneTime      bool    `json:"one_time"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		badRequest(w, "El nombre es obligatorio (como mucho 100 caracteres).")
		return
	}
	if in.DurationDays < 1 || in.DurationDays > 90 {
		badRequest(w, "La validez debe estar entre 1 y 90 días.")
		return
	}
	var gid *uuid.UUID
	if in.GroupID != nil && *in.GroupID != "" {
		id, err := uuid.Parse(*in.GroupID)
		if err != nil {
			badRequest(w, "El grupo indicado no es válido.")
			return
		}
		g, err := a.d.Store.GroupByID(r.Context(), id)
		if err != nil {
			badRequest(w, "El grupo indicado no existe.")
			return
		}
		if g.Kind == "dynamic" {
			badRequest(w, "Un grupo dinámico no puede ser el destino de un token.")
			return
		}
		gid = &id
	}
	admin := adminFrom(r.Context())
	t, err := a.androidSvc().CreateEnrollmentToken(r.Context(), androidsvc.TokenRequest{Name: name, GroupID: gid,
		Duration: time.Duration(in.DurationDays) * 24 * time.Hour, OneTime: in.OneTime, By: admin.ID})
	if errors.Is(err, androidsvc.ErrNotLinked) {
		conflict(w, "Primero vincula la empresa con Google en Integraciones › Android.")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "Google no ha creado el token: "+err.Error())
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AndroidTokenCreated, TargetType: "android_token", TargetID: t.ID.String(),
		Details: map[string]any{"name": t.Name, "group": t.GroupName, "days": in.DurationDays, "one_time": t.OneTime}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toAndroidTokenJSON(t))
}

func (a *api) revokeAndroidToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.androidSvc().RevokeEnrollmentToken(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este token no existe.")
		return
	case errors.Is(err, androidsvc.ErrNotLinked):
		conflict(w, "No hay ninguna empresa vinculada con Google.")
		return
	case err != nil:
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "Google no ha revocado el token: "+err.Error())
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AndroidTokenRevoked, TargetType: "android_token", TargetID: id.String(), IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
