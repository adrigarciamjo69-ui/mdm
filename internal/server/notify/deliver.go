package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

// Deliverer envía las entregas encoladas (tarea notify.deliver) con los
// reintentos de Backoff.
type Deliverer struct {
	Store   *store.Store
	Secrets *secrets.Box
	Version string
	HTTP    *http.Client
	Now     func() time.Time
}

// Queue encola un aviso a un canal; extra son destinatarios de correo
// adicionales (de la regla de alerta).
func Queue(ctx context.Context, q store.Querier, channelID uuid.UUID, m Message, extra []string, alertIDs []int64, at time.Time) error {
	b, _ := json.Marshal(m)
	_, err := store.QueueDelivery(ctx, q, store.Delivery{ChannelID: channelID, Event: m.Event, Message: b, ExtraTo: extra, AlertIDs: alertIDs}, at)
	return err
}

// SendTo envía un mensaje a un canal en el acto (botón «Enviar prueba»).
func (d *Deliverer) SendTo(ctx context.Context, ch store.Channel, m Message, extra []string) error {
	switch ch.Type {
	case "webhook":
		secret, err := d.Secrets.Open(ch.SealedSecret)
		if err != nil {
			return fmt.Errorf("no se puede descifrar el secreto del webhook: %w", err)
		}
		return SendWebhook(ctx, d.HTTP, ch.URL, string(secret), m, d.Version)
	case "email":
		cfg, err := d.SMTP(ctx)
		if err != nil {
			return err
		}
		return SendEmail(ctx, cfg, append(append([]string{}, ch.Recipients...), extra...), m)
	}
	return errors.New("tipo de canal desconocido")
}

// SMTP devuelve la configuración de correo con la contraseña descifrada.
func (d *Deliverer) SMTP(ctx context.Context) (SMTP, error) {
	c, err := d.Store.SMTPConfig(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return SMTP{}, errors.New("el correo no está configurado (Integraciones › Correo)")
	}
	if err != nil {
		return SMTP{}, err
	}
	pass := ""
	if c.SealedPassword != "" {
		b, err := d.Secrets.Open(c.SealedPassword)
		if err != nil {
			return SMTP{}, fmt.Errorf("no se puede descifrar la contraseña del correo: %w", err)
		}
		pass = string(b)
	}
	return SMTP{Host: c.Host, Port: c.Port, Security: c.Security, Username: c.Username, Password: pass, From: c.FromAddress, FromName: c.FromName}, nil
}

// Handle procesa una entrega (un intento).
func (d *Deliverer) Handle(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		DeliveryID int64 `json:"delivery_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	del, err := d.Store.GetDelivery(ctx, p.DeliveryID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && del.Status != "pending") {
		return nil
	}
	if err != nil {
		return err
	}
	ch, err := d.Store.GetChannel(ctx, del.ChannelID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var m Message
	_ = json.Unmarshal(del.Message, &m)
	now := d.Now()
	sendErr := d.SendTo(ctx, ch, m, del.ExtraTo)
	if sendErr == nil {
		_ = d.Store.SetChannelError(ctx, ch.ID, "", now)
		return d.Store.FinishDelivery(ctx, del.ID, "", nil, now)
	}
	var retry *time.Time
	if wait, ok := Backoff(del.Attempts + 1); ok {
		t := now.Add(wait)
		retry = &t
	} else {
		_ = d.Store.SetChannelError(ctx, ch.ID, sendErr.Error(), now)
	}
	return d.Store.FinishDelivery(ctx, del.ID, sendErr.Error(), retry, now)
}
