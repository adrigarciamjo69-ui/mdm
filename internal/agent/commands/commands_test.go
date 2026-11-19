package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

type call struct {
	kind string // "start" o "finish"
	id   string
	res  protocol.CommandResult
}

type fakeAPI struct {
	mu        sync.Mutex
	calls     []call
	finishErr error
}

func (f *fakeAPI) StartCommand(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{kind: "start", id: id})
	return nil
}

func (f *fakeAPI) FinishCommand(_ context.Context, id string, r protocol.CommandResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{kind: "finish", id: id, res: r})
	return f.finishErr
}

func (f *fakeAPI) snapshot() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

type harness struct {
	api       *fakeAPI
	runner    *Runner
	execs     int
	powers    []string
	events    []string
	mu        sync.Mutex
	statePath string
	cancel    context.CancelFunc
	done      chan struct{}
	now       time.Time
}

func newHarness(t *testing.T) *harness {
	h := &harness{api: &fakeAPI{}, statePath: filepath.Join(t.TempDir(), "commands.json"), now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	h.runner = h.build(t)
	return h
}

func (h *harness) build(t *testing.T) *Runner {
	r, err := New(Deps{
		API: h.api,
		Exec: func(ctx context.Context, s exec.Spec) exec.Result {
			h.mu.Lock()
			h.execs++
			h.mu.Unlock()
			if s.Content == "lento" {
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
				}
			}
			code := 0
			if s.Content == "falla" {
				code = 2
			}
			return exec.Result{ExitCode: code, Stdout: "salida de " + s.Content, Started: h.now, Finished: h.now}
		},
		Power: func(action string, delay time.Duration, msg string) error {
			h.mu.Lock()
			h.powers = append(h.powers, fmt.Sprintf("%s %v %s", action, delay, msg))
			h.mu.Unlock()
			return nil
		},
		RefreshInventory: func() { h.mu.Lock(); h.events = append(h.events, "inventory"); h.mu.Unlock() },
		EvaluatePolicies: func() { h.mu.Lock(); h.events = append(h.events, "policies"); h.mu.Unlock() },
		Retire: func() error {
			h.mu.Lock()
			h.events = append(h.events, fmt.Sprintf("retire después de %d llamadas", len(h.api.snapshot())))
			h.mu.Unlock()
			return nil
		},
		StatePath: h.statePath,
		Now:       func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (h *harness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan struct{})
	go func() { h.runner.Run(ctx); close(h.done) }()
}

func (h *harness) stop() { h.cancel(); <-h.done }

func (h *harness) waitFinished(t *testing.T, n int) []call {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var fin []call
		for _, c := range h.api.snapshot() {
			if c.kind == "finish" {
				fin = append(fin, c)
			}
		}
		if len(fin) >= n {
			return fin
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no llegaron %d resultados: %+v", n, h.api.snapshot())
	return nil
}

func cmd(id, typ string, payload any) protocol.Command {
	b, _ := json.Marshal(payload)
	return protocol.Command{ID: id, Type: typ, Payload: b, ExpiresAt: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
}

func script(id, content string) protocol.Command {
	return cmd(id, protocol.CmdRunScript, protocol.ScriptPayload{Interpreter: "sh", Content: content, TimeoutS: 60})
}

func TestRunner_ExecutesScriptAndReports(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "ok")})
	fin := h.waitFinished(t, 1)
	calls := h.api.snapshot()
	if calls[0].kind != "start" || fin[0].res.Status != "succeeded" || *fin[0].res.ExitCode != 0 || fin[0].res.Stdout != "salida de ok" {
		t.Fatalf("%+v", calls)
	}
}

func TestRunner_FailedScript(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "falla")})
	fin := h.waitFinished(t, 1)
	if fin[0].res.Status != "failed" || *fin[0].res.ExitCode != 2 {
		t.Fatalf("%+v", fin[0].res)
	}
}

func TestRunner_NoRepeatAfterRestart(t *testing.T) {
	h := newHarness(t)
	// El resultado no llega al servidor (sin red) y el agente se reinicia.
	h.api.finishErr = errors.New("sin red")
	h.start()
	h.runner.Submit([]protocol.Command{script("c1", "ok")})
	h.waitFinished(t, 1)
	h.stop()
	// Tras el reinicio el servidor vuelve a entregar la orden.
	h.api.calls, h.api.finishErr = nil, nil
	h.runner = h.build(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "ok")})
	fin := h.waitFinished(t, 1)
	if h.execs != 1 {
		t.Fatalf("la orden se ejecutó %d veces", h.execs)
	}
	if fin[0].res.Status != "succeeded" {
		t.Fatalf("debe reenviar el resultado guardado: %+v", fin[0].res)
	}
}

func TestRunner_InterruptedReportsFailed(t *testing.T) {
	h := newHarness(t)
	// Simula un agente que murió a mitad: el id consta como iniciado y sin resultado.
	if err := h.runner.markStarted("c9"); err != nil {
		t.Fatal(err)
	}
	h.runner = h.build(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c9", "ok")})
	fin := h.waitFinished(t, 1)
	if h.execs != 0 || fin[0].res.Status != "failed" || fin[0].res.Error != "Interrumpida por un reinicio del agente." {
		t.Fatalf("execs=%d %+v", h.execs, fin[0].res)
	}
}

func TestRunner_DuplicateInSameDelivery(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "ok"), script("c1", "ok")})
	h.runner.Submit([]protocol.Command{script("c1", "ok")})
	h.waitFinished(t, 1)
	time.Sleep(100 * time.Millisecond)
	if h.execs != 1 {
		t.Fatalf("ejecuciones: %d", h.execs)
	}
}

func TestRunner_RemembersLast500(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 510; i++ {
		_ = h.runner.markStarted(fmt.Sprintf("c%d", i))
	}
	r := h.build(t)
	if r.started("c5") || !r.started("c509") || !r.started("c10") {
		t.Fatal("debe recordar los últimos 500 ids")
	}
}

func TestRunner_ResultRetriedUntilConfirmed(t *testing.T) {
	h := newHarness(t)
	h.api.finishErr = errors.New("sin red")
	h.runner.retryEvery = 50 * time.Millisecond
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "ok")})
	h.waitFinished(t, 1)
	h.api.mu.Lock()
	h.api.finishErr = nil
	h.api.mu.Unlock()
	fin := h.waitFinished(t, 2)
	if h.execs != 1 || fin[1].id != "c1" {
		t.Fatalf("el resultado debe reenviarse sin volver a ejecutar: execs=%d", h.execs)
	}
}

func TestRunner_ExpiredSkipped(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	c := script("c1", "ok")
	c.ExpiresAt = h.now.Add(-time.Minute)
	h.runner.Submit([]protocol.Command{c, cmd("c2", protocol.CmdRefreshInventory, nil)})
	h.waitFinished(t, 1)
	if h.execs != 0 {
		t.Fatal("una orden caducada no se ejecuta")
	}
}

func TestRunner_UnknownType(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{cmd("c1", "formatear", nil)})
	fin := h.waitFinished(t, 1)
	if fin[0].res.Status != "failed" || fin[0].res.Error != "Tipo de orden no admitido: formatear." {
		t.Fatalf("%+v", fin[0].res)
	}
}

func TestRunner_PowerAndHooks(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{
		cmd("c1", protocol.CmdReboot, protocol.PowerPayload{DelayS: 300, Message: "Aviso"}),
		cmd("c2", protocol.CmdShutdown, protocol.PowerPayload{}),
		cmd("c3", protocol.CmdRefreshInventory, nil),
		cmd("c4", protocol.CmdEvaluatePolicies, nil),
	})
	fin := h.waitFinished(t, 4)
	for _, f := range fin {
		if f.res.Status != "succeeded" {
			t.Errorf("%s: %+v", f.id, f.res)
		}
	}
	if fmt.Sprint(h.powers) != "[reboot 5m0s Aviso shutdown 0s ]" || fmt.Sprint(h.events) != "[inventory policies]" {
		t.Fatalf("powers=%v events=%v", h.powers, h.events)
	}
}

func TestRunner_RetireReportsFirst(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{cmd("c1", protocol.CmdRetire, nil)})
	h.waitFinished(t, 1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		n := len(h.events)
		h.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(h.events) != 1 || h.events[0] != "retire después de 2 llamadas" {
		t.Fatalf("el resultado debe enviarse antes de desinstalar: %v", h.events)
	}
}

func TestRunner_DoesNotBlockSubmit(t *testing.T) {
	h := newHarness(t)
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{script("c1", "lento")})
	start := time.Now()
	h.runner.Submit([]protocol.Command{script("c2", "ok")})
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Submit no debe esperar a la orden en curso")
	}
	h.waitFinished(t, 2)
}

func TestRunner_PatchCommands(t *testing.T) {
	h := newHarness(t)
	var calls []string
	h.runner.d.ScanUpdates = func() { calls = append(calls, "scan") }
	h.runner.d.InstallUpdatesNow = func() { calls = append(calls, "install") }
	h.start()
	defer h.stop()
	h.runner.Submit([]protocol.Command{cmd("c1", protocol.CmdScanUpdates, nil), cmd("c2", protocol.CmdInstallUpdatesNow, nil)})
	fin := h.waitFinished(t, 2)
	if fin[0].res.Status != "succeeded" || fmt.Sprint(calls) != "[scan install]" {
		t.Fatalf("%v %+v", calls, fin)
	}
}
