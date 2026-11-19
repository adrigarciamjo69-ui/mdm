package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Sucesos internos. Se publican en la cola de tareas (events.dispatch) dentro
// de la misma transacción que el cambio que los origina: un suceso nunca se
// pierde ni se emite por un cambio que no llegó a guardarse.
const (
	EventDeviceEnrolled    = "device.enrolled"
	EventDeviceReenrolled  = "device.reenrolled"
	EventComplianceChanged = "compliance.changed"
	EventDeploymentFailed  = "deployment.failed"
	EventCommandFailed     = "command.failed"
	EventLoginFailed       = "login.failed"
	EventAlertOpened       = "alert.opened"
)

type Event struct {
	Type     string         `json:"type"`
	DeviceID *uuid.UUID     `json:"device_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	At       time.Time      `json:"at"`
}

// emit encola el suceso con q (que puede ser una transacción).
func emit(ctx context.Context, q Querier, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	return EnqueueJob(ctx, q, NewJob{Kind: "events.dispatch", Payload: e, RunAt: e.At})
}

// PublishEvent publica un suceso que no forma parte de otro cambio (por
// ejemplo, un inicio de sesión fallido).
func (s *Store) PublishEvent(ctx context.Context, e Event) error { return emit(ctx, s.pool, e) }

// PublishEventTx publica un suceso dentro de una transacción en curso.
func PublishEventTx(ctx context.Context, q Querier, e Event) error { return emit(ctx, q, e) }

func devicePtr(id uuid.UUID) *uuid.UUID { return &id }
