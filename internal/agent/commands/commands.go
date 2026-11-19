// Package commands ejecuta en el agente las órdenes que llegan con el
// check-in: una a la vez, en paralelo al bucle, sin repetir ninguna aunque el
// agente se reinicie y sin perder resultados si falla la red.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

const (
	rememberIDs = 500
	queueSize   = 100
)

type API interface {
	StartCommand(ctx context.Context, id string) error
	FinishCommand(ctx context.Context, id string, r protocol.CommandResult) error
}

type Deps struct {
	API              API
	Exec             func(ctx context.Context, s exec.Spec) exec.Result
	Power            func(action string, delay time.Duration, message string) error
	RefreshInventory func()
	EvaluatePolicies func()
	// ScanUpdates e InstallUpdatesNow piden al gestor de parches que actúe ya.
	ScanUpdates       func()
	InstallUpdatesNow func()
	// Retire desinstala el agente. Se llama después de informar del resultado.
	Retire func() error
	// StatePath es el fichero donde se recuerdan las órdenes iniciadas y los
	// resultados aún no confirmados por el servidor.
	StatePath string
	ScriptDir string
	Now       func() time.Time
	Log       *slog.Logger
}

type persisted struct {
	Started []string                          `json:"started"`
	Results map[string]protocol.CommandResult `json:"results"`
}

type Runner struct {
	d          Deps
	retryEvery time.Duration

	mu     sync.Mutex
	st     persisted
	queued map[string]bool
	queue  chan protocol.Command
}

// New crea el ejecutor cargando el estado guardado.
func New(d Deps) (*Runner, error) {
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	r := &Runner{d: d, retryEvery: time.Minute, queued: map[string]bool{}, queue: make(chan protocol.Command, queueSize),
		st: persisted{Results: map[string]protocol.CommandResult{}}}
	b, err := os.ReadFile(d.StatePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &r.st); err != nil {
			d.Log.Warn("estado de órdenes ilegible; se empieza de cero", "err", err)
			r.st = persisted{}
		}
		if r.st.Results == nil {
			r.st.Results = map[string]protocol.CommandResult{}
		}
	}
	return r, nil
}

// save escribe el estado de forma atómica. Llamar con mu tomado.
func (r *Runner) save() error {
	b, _ := json.Marshal(r.st)
	tmp, err := os.CreateTemp(filepath.Dir(r.d.StatePath), "commands.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), r.d.StatePath)
}

func (r *Runner) markStarted(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st.Started = append(r.st.Started, id)
	if len(r.st.Started) > rememberIDs {
		r.st.Started = r.st.Started[len(r.st.Started)-rememberIDs:]
	}
	return r.save()
}

func (r *Runner) started(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.st.Started {
		if s == id {
			return true
		}
	}
	return false
}

func (r *Runner) storeResult(id string, res protocol.CommandResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st.Results[id] = res
	if err := r.save(); err != nil {
		r.d.Log.Error("no se pudo guardar el resultado", "id", id, "err", err)
	}
}

func (r *Runner) storedResult(id string) (protocol.CommandResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.st.Results[id]
	return res, ok
}

func (r *Runner) forget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Results[id]; ok {
		delete(r.st.Results, id)
		_ = r.save()
	}
}

// Submit encola órdenes sin esperar. Las ya encoladas se ignoran.
func (r *Runner) Submit(cmds []protocol.Command) {
	for _, c := range cmds {
		r.mu.Lock()
		dup := r.queued[c.ID]
		if !dup {
			r.queued[c.ID] = true
		}
		r.mu.Unlock()
		if dup {
			continue
		}
		select {
		case r.queue <- c:
		default: // cola llena: el servidor la volverá a entregar
			r.mu.Lock()
			delete(r.queued, c.ID)
			r.mu.Unlock()
		}
	}
}

// Run procesa órdenes hasta que se cancela el contexto y reintenta enviar los
// resultados que el servidor no confirmó.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(r.retryEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.retryPending(ctx)
		case c := <-r.queue:
			r.handle(ctx, c)
			r.mu.Lock()
			delete(r.queued, c.ID)
			r.mu.Unlock()
		}
	}
}

func (r *Runner) retryPending(ctx context.Context) {
	r.mu.Lock()
	pending := map[string]protocol.CommandResult{}
	for id, res := range r.st.Results {
		pending[id] = res
	}
	r.mu.Unlock()
	for id, res := range pending {
		r.report(ctx, id, res)
	}
}

func (r *Runner) report(ctx context.Context, id string, res protocol.CommandResult) {
	if err := r.d.API.FinishCommand(ctx, id, res); err != nil {
		r.d.Log.Warn("no se pudo enviar el resultado; se reintentará", "id", id, "err", err)
		return
	}
	r.forget(id)
}

func (r *Runner) failed(msg string) protocol.CommandResult {
	now := r.d.Now()
	return protocol.CommandResult{Status: protocol.CommandFailed, Error: msg, StartedAt: now, FinishedAt: now}
}

func (r *Runner) handle(ctx context.Context, c protocol.Command) {
	if r.started(c.ID) {
		// Ya se inició en una ejecución anterior: nunca se repite.
		res, ok := r.storedResult(c.ID)
		if !ok {
			res = r.failed("Interrumpida por un reinicio del agente.")
			r.storeResult(c.ID, res)
		}
		r.report(ctx, c.ID, res)
		return
	}
	if !c.ExpiresAt.IsZero() && r.d.Now().After(c.ExpiresAt) {
		return
	}
	if err := r.markStarted(c.ID); err != nil {
		// Sin poder recordar que se inició, ejecutar arriesga repetirla.
		r.d.Log.Error("no se pudo guardar el estado; la orden no se ejecuta", "id", c.ID, "err", err)
		return
	}
	if err := r.d.API.StartCommand(ctx, c.ID); err != nil {
		r.d.Log.Warn("no se pudo confirmar el inicio", "id", c.ID, "err", err)
	}
	r.d.Log.Info("ejecutando orden", "id", c.ID, "tipo", c.Type)
	res := r.execute(ctx, c)
	r.storeResult(c.ID, res)
	r.report(ctx, c.ID, res)
	if c.Type == protocol.CmdRetire && res.Status == protocol.CommandSucceeded {
		go func() {
			if err := r.d.Retire(); err != nil {
				r.d.Log.Error("no se pudo desinstalar el agente", "err", err)
			}
		}()
	}
}

func (r *Runner) execute(ctx context.Context, c protocol.Command) protocol.CommandResult {
	start := r.d.Now()
	ok := func() protocol.CommandResult {
		return protocol.CommandResult{Status: protocol.CommandSucceeded, StartedAt: start, FinishedAt: r.d.Now()}
	}
	switch c.Type {
	case protocol.CmdReboot, protocol.CmdShutdown:
		var p protocol.PowerPayload
		_ = json.Unmarshal(c.Payload, &p)
		if err := r.d.Power(c.Type, time.Duration(p.DelayS)*time.Second, p.Message); err != nil {
			return r.failed(err.Error())
		}
		return ok()
	case protocol.CmdRefreshInventory:
		r.d.RefreshInventory()
		return ok()
	case protocol.CmdEvaluatePolicies:
		r.d.EvaluatePolicies()
		return ok()
	case protocol.CmdScanUpdates, protocol.CmdInstallUpdatesNow:
		f := r.d.ScanUpdates
		if c.Type == protocol.CmdInstallUpdatesNow {
			f = r.d.InstallUpdatesNow
		}
		if f == nil {
			return r.failed("Este agente no gestiona actualizaciones.")
		}
		f()
		return ok()
	case protocol.CmdRetire:
		return ok()
	case protocol.CmdRunScript:
		var p protocol.ScriptPayload
		if err := json.Unmarshal(c.Payload, &p); err != nil {
			return r.failed("La orden no trae un script válido.")
		}
		res := r.d.Exec(ctx, exec.Spec{Interpreter: p.Interpreter, Content: p.Content, Env: p.Env,
			Timeout: time.Duration(p.TimeoutS) * time.Second, ScriptDir: r.d.ScriptDir})
		code := res.ExitCode
		out := protocol.CommandResult{Status: protocol.CommandSucceeded, ExitCode: &code, Stdout: res.Stdout, Stderr: res.Stderr,
			StartedAt: res.Started, FinishedAt: res.Finished}
		switch {
		case res.Err != nil:
			out.Status, out.ExitCode, out.Error = protocol.CommandFailed, nil, res.Err.Error()
		case res.TimedOut:
			out.Status, out.Error = protocol.CommandFailed, fmt.Sprintf("Tiempo agotado tras %d s.", p.TimeoutS)
		case code != 0:
			out.Status = protocol.CommandFailed
		}
		return out
	}
	return r.failed(fmt.Sprintf("Tipo de orden no admitido: %s.", c.Type))
}
