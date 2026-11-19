package adminapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/notify"
	"mdm/internal/server/store"
)

func (a *api) deliverer() *notify.Deliverer {
	v := a.d.Version
	if v == "" {
		v = "dev"
	}
	return &notify.Deliverer{Store: a.d.Store, Secrets: a.d.Secrets, Version: v, Now: a.d.Now}
}

func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && len(s) <= 254
}

func (a *api) getEmailConfig(w http.ResponseWriter, r *http.Request) {
	c, err := a.d.Store.SMTPConfig(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"configured": true, "host": c.Host, "port": c.Port, "security": c.Security,
		"username": c.Username, "has_password": c.SealedPassword != "", "from_address": c.FromAddress, "from_name": c.FromName})
}

func (a *api) putEmailConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Security    string `json:"security"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		FromAddress string `json:"from_address"`
		FromName    string `json:"from_name"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	in.Host = strings.TrimSpace(in.Host)
	switch {
	case in.Host == "" || strings.ContainsAny(in.Host, " /"):
		badRequest(w, "Indica el servidor de correo (por ejemplo, smtp.office365.com).")
		return
	case in.Port < 1 || in.Port > 65535:
		badRequest(w, "El puerto debe estar entre 1 y 65535 (habitualmente 587 con STARTTLS o 465 con TLS).")
		return
	case in.Security != "starttls" && in.Security != "tls" && in.Security != "none":
		badRequest(w, "La seguridad debe ser starttls, tls o none.")
		return
	case !validEmail(in.FromAddress):
		badRequest(w, "La dirección del remitente no es válida.")
		return
	case strings.ContainsAny(in.FromName+in.Username, "\r\n"):
		badRequest(w, "El nombre y el usuario no pueden tener saltos de línea.")
		return
	}
	sealed := ""
	prev, perr := a.d.Store.SMTPConfig(r.Context())
	switch {
	case in.Password == secretMask && perr == nil:
		sealed = prev.SealedPassword
	case in.Password != "" && in.Password != secretMask:
		var err error
		if sealed, err = a.d.Secrets.Seal([]byte(in.Password)); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if err := a.d.Store.SaveSMTPConfig(r.Context(), store.SMTPConfig{Host: in.Host, Port: in.Port, Security: in.Security, Username: strings.TrimSpace(in.Username),
		SealedPassword: sealed, FromAddress: in.FromAddress, FromName: strings.TrimSpace(in.FromName)}); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.EmailConfigured, TargetType: "integration", TargetID: "email",
		Details: map[string]any{"host": in.Host, "port": in.Port, "security": in.Security}, IP: a.ip(r)})
	a.getEmailConfig(w, r)
}

func (a *api) testEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	if !validEmail(in.To) {
		badRequest(w, "Indica una dirección de correo válida para la prueba.")
		return
	}
	d := a.deliverer()
	cfg, err := d.SMTP(r.Context())
	if err == nil {
		err = notify.SendEmail(r.Context(), cfg, []string{in.To}, notify.Message{Subject: "Prueba del MDM",
			Text: "Este es un correo de prueba del MDM. Si lo recibes, el correo está bien configurado.",
			HTML: "<p>Este es un correo de prueba del MDM. Si lo recibes, el correo está bien configurado.</p>"})
	}
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "No se ha podido enviar: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type channelJSON struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Recipients  []string   `json:"recipients"`
	URL         string     `json:"url"`
	Secret      string     `json:"secret,omitempty"`
	LastError   string     `json:"last_error"`
	LastErrorAt *time.Time `json:"last_error_at"`
}

func toChannelJSON(c store.Channel) channelJSON {
	return channelJSON{ID: c.ID.String(), Name: c.Name, Type: c.Type, Recipients: c.Recipients, URL: c.URL, LastError: c.LastError, LastErrorAt: c.LastErrorAt}
}

func (a *api) listChannels(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Store.ListChannels(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]channelJSON, 0, len(list))
	for _, c := range list {
		out = append(out, toChannelJSON(c))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type channelInput struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Recipients []string `json:"recipients"`
	URL        string   `json:"url"`
}

func validateChannel(in *channelInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return errors.New("El nombre es obligatorio (como mucho 100 caracteres).")
	}
	switch in.Type {
	case "email":
		if len(in.Recipients) == 0 || len(in.Recipients) > 50 {
			return errors.New("Indica entre 1 y 50 destinatarios.")
		}
		for _, r := range in.Recipients {
			if !validEmail(r) {
				return errors.New("La dirección «" + r + "» no es válida.")
			}
		}
		in.URL = ""
	case "webhook":
		if err := notify.ValidateWebhookURL(in.URL); err != nil {
			return err
		}
		in.Recipients = nil
	default:
		return errors.New("El tipo de canal debe ser email o webhook.")
	}
	return nil
}

func (a *api) createChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	if err := validateChannel(&in); err != nil {
		badRequest(w, err.Error())
		return
	}
	secret, sealed := "", ""
	if in.Type == "webhook" {
		secret = notify.NewSecret()
		var err error
		if sealed, err = a.d.Secrets.Seal([]byte(secret)); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	c, err := a.d.Store.CreateChannel(r.Context(), store.Channel{Name: in.Name, Type: in.Type, Recipients: in.Recipients, URL: in.URL, SealedSecret: sealed})
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya hay un canal con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ChannelCreated, TargetType: "channel", TargetID: c.ID.String(),
		Details: map[string]any{"name": c.Name, "type": c.Type}, IP: a.ip(r)})
	out := toChannelJSON(c)
	out.Secret = secret // solo ahora: después no se puede volver a ver
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *api) loadChannel(w http.ResponseWriter, r *http.Request) (store.Channel, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.Channel{}, false
	}
	c, err := a.d.Store.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este canal no existe.")
		return c, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return c, false
	}
	return c, true
}

func (a *api) updateChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	var in channelInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	in.Type = c.Type
	if err := validateChannel(&in); err != nil {
		badRequest(w, err.Error())
		return
	}
	c.Name, c.Recipients, c.URL = in.Name, in.Recipients, in.URL
	if err := a.d.Store.UpdateChannel(r.Context(), c); errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya hay un canal con ese nombre.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ChannelUpdated, TargetType: "channel", TargetID: c.ID.String(),
		Details: map[string]any{"name": c.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, toChannelJSON(c))
}

func (a *api) deleteChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteChannel(r.Context(), c.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ChannelDeleted, TargetType: "channel", TargetID: c.ID.String(),
		Details: map[string]any{"name": c.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) testChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	now := a.d.Now().UTC()
	m := notify.Message{Event: "test", Subject: "Prueba del canal «" + c.Name + "»",
		Text: "Mensaje de prueba del MDM.", HTML: "<p>Mensaje de prueba del MDM.</p>",
		Payload: map[string]any{"event": "test", "sent_at": now, "channel": c.Name}}
	if err := a.deliverer().SendTo(r.Context(), c, m, nil); err != nil {
		_ = a.d.Store.SetChannelError(r.Context(), c.ID, err.Error(), now)
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "La prueba ha fallado: "+err.Error())
		return
	}
	_ = a.d.Store.SetChannelError(r.Context(), c.ID, "", now)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"result": "Enviado."})
}
