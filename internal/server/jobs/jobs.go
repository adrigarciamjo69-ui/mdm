// Package jobs ejecuta tareas en segundo plano guardadas en PostgreSQL: una
// cola compartida por todas las réplicas del servidor, con reintentos y
// tareas periódicas.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"mdm/internal/server/store"
)

const (
	lockFor   = 5 * time.Minute
	renewEach = time.Minute
	idleWait  = time.Second
)

// Handler procesa una tarea. Un error provoca un reintento.
type Handler func(ctx context.Context, payload json.RawMessage) error

type Runner struct {
	store    *store.Store
	now      func() time.Time
	log      *slog.Logger
	handlers map[string]Handler
	periodic map[string]time.Duration
}

func New(s *store.Store, now func() time.Time, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{store: s, now: now, log: log, handlers: map[string]Handler{}, periodic: map[string]time.Duration{}}
}

// Handle registra el manejador de un tipo de tarea.
func (r *Runner) Handle(kind string, h Handler) { r.handlers[kind] = h }

// Every declara una tarea periódica. Debe tener manejador.
func (r *Runner) Every(kind string, every time.Duration) { r.periodic[kind] = every }

func periodicKey(kind string) string { return "periodic:" + kind }

// SchedulePeriodic encola la primera ejecución de cada tarea periódica que no
// tenga ya una pendiente. Se llama al arrancar.
func (r *Runner) SchedulePeriodic(ctx context.Context) error {
	for kind := range r.periodic {
		if err := store.EnqueueJob(ctx, r.store.Pool(), store.NewJob{Kind: kind, RunAt: r.now(), UniqueKey: periodicKey(kind)}); err != nil {
			return err
		}
	}
	return nil
}

// Run procesa tareas hasta que se cancela el contexto.
func (r *Runner) Run(ctx context.Context) {
	if err := r.SchedulePeriodic(ctx); err != nil {
		r.log.Error("tareas periódicas", "err", err)
	}
	for ctx.Err() == nil {
		ran, err := r.RunOnce(ctx)
		if err != nil {
			r.log.Error("cola de tareas", "err", err)
		}
		if !ran || err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(idleWait):
			}
		}
	}
}

// backoff devuelve la espera tras el intento n (1, 2, 3…): 30 s, 2 min, 8 min, 32 min…
func backoff(attempt int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 4
	}
	return d
}

// RunOnce ejecuta como mucho una tarea. Devuelve si había alguna lista.
func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	j, err := r.store.ClaimJob(ctx, r.now(), lockFor)
	if err != nil || j == nil {
		return false, err
	}
	h, ok := r.handlers[j.Kind]
	var runErr error
	if !ok {
		runErr = fmt.Errorf("tipo de tarea desconocido: %s", j.Kind)
		j.Attempts = j.MaxAttempts // no tiene sentido reintentar
	} else {
		runErr = r.execute(ctx, j, h)
	}

	now := r.now()
	switch {
	case runErr == nil:
		err = r.store.FinishJob(ctx, j.ID, now)
	case j.Attempts < j.MaxAttempts:
		r.log.Warn("tarea fallida, se reintentará", "kind", j.Kind, "id", j.ID, "intento", j.Attempts, "err", runErr)
		err = r.store.RetryJob(ctx, j.ID, runErr.Error(), now.Add(backoff(j.Attempts)))
	default:
		r.log.Error("tarea fallida definitivamente", "kind", j.Kind, "id", j.ID, "err", runErr)
		err = r.store.FailJob(ctx, j.ID, runErr.Error(), now)
	}
	if err != nil {
		return true, err
	}
	// Una periódica se reprograma al terminar, haya ido bien o se haya rendido.
	if every, ok := r.periodic[j.Kind]; ok && (runErr == nil || j.Attempts >= j.MaxAttempts) {
		err = store.EnqueueJob(ctx, r.store.Pool(), store.NewJob{Kind: j.Kind, RunAt: now.Add(every), UniqueKey: periodicKey(j.Kind)})
	}
	return true, err
}

// execute corre el manejador renovando el bloqueo y convirtiendo un pánico en error.
func (r *Runner) execute(ctx context.Context, j *store.Job, h Handler) (err error) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(renewEach)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = r.store.ExtendJobLock(context.WithoutCancel(ctx), j.ID, r.now().Add(lockFor))
			}
		}
	}()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("pánico: %v", p)
		}
	}()
	return h(ctx, j.Payload)
}
