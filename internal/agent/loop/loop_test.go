package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"mdm/internal/agent/client"
	"mdm/internal/agent/config"
	"mdm/internal/protocol"
)

type fakeAPI struct {
	checkins    []protocol.CheckinRequest
	inventories []protocol.Inventory
	// respuestas y errores por orden de llamada; al agotarse se repite la última
	responses    []protocol.CheckinResponse
	checkinErrs  []error
	inventoryErr error
	events       *[]string
}

func (f *fakeAPI) Checkin(_ context.Context, r protocol.CheckinRequest) (protocol.CheckinResponse, error) {
	i := len(f.checkins)
	f.checkins = append(f.checkins, r)
	*f.events = append(*f.events, "checkin")
	var err error
	if len(f.checkinErrs) > 0 {
		err = f.checkinErrs[min(i, len(f.checkinErrs)-1)]
	}
	var resp protocol.CheckinResponse
	if len(f.responses) > 0 {
		resp = f.responses[min(i, len(f.responses)-1)]
	}
	return resp, err
}

func (f *fakeAPI) SendInventory(_ context.Context, inv protocol.Inventory) error {
	f.inventories = append(f.inventories, inv)
	*f.events = append(*f.events, "inventory")
	return f.inventoryErr
}

type fakeCollector struct{ collects int }

func (c *fakeCollector) Identify(context.Context) protocol.EnrollRequest {
	return protocol.EnrollRequest{}
}
func (c *fakeCollector) Collect(context.Context) protocol.Inventory {
	c.collects++
	return protocol.Inventory{Hash: "sha256:actual", System: protocol.System{Hostname: "pc"}}
}

type harness struct {
	api       *fakeAPI
	collector *fakeCollector
	sleeps    []time.Duration
	saved     []config.Config
	states    []config.State
	events    []string
	rand      float64
	now       time.Time
	deps      Deps
	cancel    context.CancelFunc
	ctx       context.Context
}

// newHarness prepara un bucle que se detiene tras maxSleeps esperas.
func newHarness(maxSleeps int) *harness {
	h := &harness{rand: 0.5, now: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), collector: &fakeCollector{}}
	h.api = &fakeAPI{events: &h.events}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.deps = Deps{
		API: h.api, Collector: h.collector, Version: "1.0.0",
		Config:     config.Config{ServerURL: "https://mdm", DeviceID: "d", Credential: "s", CheckinIntervalS: 60, InventoryIntervalS: 21600},
		SaveConfig: func(c config.Config) error { h.saved = append(h.saved, c); return nil },
		SaveState:  func(s config.State) error { h.states = append(h.states, s); return nil },
		Sleep: func(ctx context.Context, d time.Duration) error {
			h.sleeps = append(h.sleeps, d)
			h.events = append(h.events, "sleep")
			h.now = h.now.Add(d)
			if len(h.sleeps) >= maxSleeps {
				h.cancel()
				return ctx.Err()
			}
			return nil
		},
		Rand: func() float64 { return h.rand },
		Now:  func() time.Time { return h.now },
	}
	return h
}

func (h *harness) run(t *testing.T) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- Run(h.ctx, h.deps) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run devolvió error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run no terminó")
	}
}

var tmpErr = &client.TemporaryError{Err: errors.New("sin red")}

func TestLoop_ChecksInAtInterval(t *testing.T) {
	h := newHarness(3)
	h.run(t)
	if len(h.api.checkins) != 3 {
		t.Fatalf("check-ins: %d", len(h.api.checkins))
	}
	for i, d := range h.sleeps {
		if d != 60*time.Second {
			t.Errorf("espera %d: %v", i, d)
		}
	}
}

func TestLoop_JitterTenPercent(t *testing.T) {
	h := newHarness(1)
	h.rand = 0
	h.run(t)
	if h.sleeps[0] != 54*time.Second {
		t.Errorf("Rand=0: %v", h.sleeps[0])
	}
	h = newHarness(1)
	h.rand = 1
	h.run(t)
	if h.sleeps[0] != 66*time.Second {
		t.Errorf("Rand=1: %v", h.sleeps[0])
	}
}

func TestLoop_FirstCheckinImmediate(t *testing.T) {
	h := newHarness(1)
	h.run(t)
	if len(h.events) < 2 || h.events[0] != "checkin" {
		t.Fatalf("orden de sucesos: %v", h.events)
	}
}

func TestLoop_SendsInventoryWhenAsked(t *testing.T) {
	h := newHarness(1)
	h.api.responses = []protocol.CheckinResponse{{SendInventory: true}}
	h.run(t)
	if len(h.api.inventories) != 1 || h.api.inventories[0].Hash != "sha256:actual" || h.api.inventories[0].System.Hostname != "pc" {
		t.Fatalf("inventarios: %+v", h.api.inventories)
	}
}

func TestLoop_NoInventoryWhenNotAsked(t *testing.T) {
	h := newHarness(3)
	h.run(t)
	if len(h.api.inventories) != 0 {
		t.Fatalf("envió inventario sin que se lo pidieran: %d", len(h.api.inventories))
	}
}

func TestLoop_CheckinCarriesCurrentHashAndVersion(t *testing.T) {
	h := newHarness(1)
	h.run(t)
	if c := h.api.checkins[0]; c.InventoryHash != "sha256:actual" || c.AgentVersion != "1.0.0" {
		t.Fatalf("check-in: %+v", c)
	}
}

func TestLoop_CollectsAtMostEveryFiveMinutes(t *testing.T) {
	h := newHarness(11) // 11 esperas de 60 s: 11 check-ins en 10 minutos
	h.run(t)
	if h.collector.collects < 2 || h.collector.collects > 3 {
		t.Fatalf("recogidas de inventario en 10 minutos: %d (se esperaban 2 o 3)", h.collector.collects)
	}
}

func TestLoop_AppliesServerConfig(t *testing.T) {
	h := newHarness(2)
	h.api.responses = []protocol.CheckinResponse{{Config: protocol.AgentConfig{CheckinIntervalS: 120, InventoryIntervalS: 3600}}}
	h.run(t)
	if len(h.saved) != 1 || h.saved[0].CheckinIntervalS != 120 || h.saved[0].InventoryIntervalS != 3600 {
		t.Fatalf("configuración guardada (una vez, al cambiar): %+v", h.saved)
	}
	if h.sleeps[0] != 120*time.Second || h.sleeps[1] != 120*time.Second {
		t.Fatalf("esperas: %v", h.sleeps)
	}
}

func TestLoop_IgnoresZeroOrNegativeIntervals(t *testing.T) {
	h := newHarness(2)
	h.api.responses = []protocol.CheckinResponse{{Config: protocol.AgentConfig{CheckinIntervalS: 0, InventoryIntervalS: -5}}}
	h.run(t)
	if len(h.saved) != 0 || h.sleeps[0] != 60*time.Second {
		t.Fatalf("guardado=%v esperas=%v", h.saved, h.sleeps)
	}
}

func TestLoop_BackoffOnTemporaryError(t *testing.T) {
	h := newHarness(5)
	h.api.checkinErrs = []error{tmpErr, tmpErr, tmpErr, nil}
	h.run(t)
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, w := range want {
		if h.sleeps[i] != w {
			t.Fatalf("esperas: %v, se esperaban %v", h.sleeps, want)
		}
	}
}

func TestLoop_BackoffRestartsAfterSuccess(t *testing.T) {
	h := newHarness(4)
	h.api.checkinErrs = []error{tmpErr, tmpErr, nil, tmpErr}
	h.run(t)
	if h.sleeps[3] != 5*time.Second {
		t.Fatalf("tras un éxito la espera vuelve a 5 s: %v", h.sleeps)
	}
}

func TestLoop_UnknownErrorAlsoBacksOff(t *testing.T) {
	h := newHarness(2)
	h.api.checkinErrs = []error{errors.New("el servidor rechazó la petición (400)"), nil}
	h.run(t)
	if h.sleeps[0] != 5*time.Second || len(h.api.checkins) != 2 {
		t.Fatalf("esperas: %v", h.sleeps)
	}
}

func TestLoop_RetryAfterHonored(t *testing.T) {
	h := newHarness(2)
	h.api.checkinErrs = []error{&client.RetryAfterError{After: 2 * time.Minute}, nil}
	h.run(t)
	if h.sleeps[0] != 2*time.Minute || h.sleeps[1] != 60*time.Second {
		t.Fatalf("esperas: %v", h.sleeps)
	}
}

func TestLoop_RevokedStopsSending(t *testing.T) {
	h := newHarness(4)
	h.api.checkinErrs = []error{nil, client.ErrRevoked}
	h.run(t)
	if len(h.api.checkins) != 2 {
		t.Fatalf("tras la revocación no debe haber más llamadas: %d", len(h.api.checkins))
	}
	if len(h.saved) != 1 || !h.saved[0].Revoked {
		t.Fatalf("configuración guardada: %+v", h.saved)
	}
	if len(h.sleeps) != 4 {
		t.Fatalf("el bucle debe seguir vivo, en espera: %d esperas", len(h.sleeps))
	}
	last := h.states[len(h.states)-1]
	if last.LastError == "" || last.LastErrorAt == nil {
		t.Errorf("el estado debe explicar la revocación: %+v", last)
	}
}

func TestLoop_RevokedOnInventoryAlsoStops(t *testing.T) {
	h := newHarness(3)
	h.api.responses = []protocol.CheckinResponse{{SendInventory: true}}
	h.api.inventoryErr = client.ErrRevoked
	h.run(t)
	if len(h.api.checkins) != 1 || len(h.saved) != 1 || !h.saved[0].Revoked {
		t.Fatalf("check-ins=%d guardado=%+v", len(h.api.checkins), h.saved)
	}
}

func TestLoop_StartsRevokedStaysIdle(t *testing.T) {
	h := newHarness(3)
	h.deps.Config.Revoked = true
	h.run(t)
	if len(h.api.checkins) != 0 || len(h.api.inventories) != 0 || h.collector.collects != 0 {
		t.Fatalf("un agente revocado no debe llamar al servidor: %v", h.events)
	}
}

func TestLoop_InventoryTooLargeSkipsUntilNextCycle(t *testing.T) {
	h := newHarness(2)
	h.api.responses = []protocol.CheckinResponse{{SendInventory: true}}
	h.api.inventoryErr = client.ErrTooLarge
	h.run(t)
	// Dos ciclos: un intento de inventario por ciclo, sin reintento inmediato, y el check-in sigue a su ritmo.
	if len(h.api.inventories) != 2 || len(h.api.checkins) != 2 || h.sleeps[0] != 60*time.Second {
		t.Fatalf("inventarios=%d check-ins=%d esperas=%v", len(h.api.inventories), len(h.api.checkins), h.sleeps)
	}
	want := []string{"checkin", "inventory", "sleep", "checkin", "inventory", "sleep"}
	for i, w := range want {
		if h.events[i] != w {
			t.Fatalf("sucesos: %v", h.events)
		}
	}
}

func TestLoop_InventoryTemporaryErrorKeepsCheckinPace(t *testing.T) {
	h := newHarness(2)
	h.api.responses = []protocol.CheckinResponse{{SendInventory: true}}
	h.api.inventoryErr = tmpErr
	h.run(t)
	if h.sleeps[0] != 60*time.Second || len(h.api.checkins) != 2 {
		t.Fatalf("esperas=%v", h.sleeps)
	}
}

func TestLoop_IgnoresCommands(t *testing.T) {
	h := newHarness(2)
	h.api.responses = []protocol.CheckinResponse{{Commands: []protocol.Command{{ID: "1", Type: "run_script", Payload: []byte(`{"script":"rm -rf /"}`)}}}}
	h.run(t)
	if len(h.api.checkins) != 2 || h.sleeps[0] != 60*time.Second {
		t.Fatalf("una orden no debe alterar el bucle: %v", h.events)
	}
}

func TestLoop_WritesState(t *testing.T) {
	h := newHarness(2)
	h.api.checkinErrs = []error{nil, tmpErr}
	h.run(t)
	if len(h.states) < 2 {
		t.Fatalf("estados: %+v", h.states)
	}
	first, second := h.states[0], h.states[1]
	if first.LastSuccess == nil || !first.LastSuccess.Equal(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)) || first.LastError != "" {
		t.Errorf("tras un éxito: %+v", first)
	}
	if second.LastError == "" || second.LastErrorAt == nil || second.LastSuccess == nil {
		t.Errorf("tras un fallo debe conservar el último éxito y anotar el error: %+v", second)
	}
}

func TestLoop_SaveFailuresDoNotStopLoop(t *testing.T) {
	h := newHarness(3)
	h.deps.SaveState = func(config.State) error { return errors.New("disco lleno") }
	h.deps.SaveConfig = func(config.Config) error { return errors.New("disco lleno") }
	h.api.responses = []protocol.CheckinResponse{{Config: protocol.AgentConfig{CheckinIntervalS: 120}}}
	h.run(t)
	if len(h.api.checkins) != 3 {
		t.Fatalf("check-ins: %d", len(h.api.checkins))
	}
}

func TestLoop_KeepsPreviousStateAcrossRestart(t *testing.T) {
	h := newHarness(1)
	before := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	h.deps.State = config.State{LastSuccess: &before}
	h.api.checkinErrs = []error{tmpErr}
	h.run(t)
	got := h.states[len(h.states)-1]
	if got.LastSuccess == nil || !got.LastSuccess.Equal(before) || got.LastError == "" {
		t.Fatalf("un reinicio seguido de un fallo no debe borrar la última conexión correcta: %+v", got)
	}
}

func TestLoop_DeclaresCapabilitiesAndPassesCommands(t *testing.T) {
	h := newHarness(1)
	h.deps.Capabilities = []string{"commands.v1", "scripts.v1"}
	var got []protocol.Command
	h.deps.Commands = func(c []protocol.Command) { got = append(got, c...) }
	h.api.responses = []protocol.CheckinResponse{{Commands: []protocol.Command{{ID: "c1", Type: "reboot"}}}}
	h.run(t)
	if len(h.api.checkins) != 1 || len(h.api.checkins[0].Capabilities) != 2 {
		t.Fatalf("capacidades: %+v", h.api.checkins)
	}
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("órdenes entregadas: %+v", got)
	}
}

func TestLoop_ForceInventory(t *testing.T) {
	h := newHarness(2)
	forced := true
	h.deps.ForceInventory = func() bool { f := forced; forced = false; return f }
	h.run(t)
	if len(h.api.inventories) != 1 {
		t.Fatalf("inventarios: %d", len(h.api.inventories))
	}
}

func TestLoop_PolicyHashRoundTrip(t *testing.T) {
	h := newHarness(1)
	h.deps.PolicyHash = func() string { return "sha256:evaluada" }
	var wanted string
	h.deps.PolicyWanted = func(s string) { wanted = s }
	h.api.responses = []protocol.CheckinResponse{{PolicyHash: "sha256:vigente"}}
	h.run(t)
	if h.api.checkins[0].PolicyHash != "sha256:evaluada" || wanted != "sha256:vigente" {
		t.Fatalf("enviada %q, recibida %q", h.api.checkins[0].PolicyHash, wanted)
	}
}

func TestLoop_UpdateAndAfterCheckin(t *testing.T) {
	h := newHarness(1)
	var offered []protocol.AgentUpdate
	after := 0
	h.deps.Update = func(u protocol.AgentUpdate) { offered = append(offered, u) }
	h.deps.AfterCheckin = func() { after++ }
	h.api.responses = []protocol.CheckinResponse{{AgentUpdate: &protocol.AgentUpdate{Version: "1.1.0"}}}
	h.run(t)
	if len(offered) != 1 || offered[0].Version != "1.1.0" || after != 1 {
		t.Fatalf("ofertas=%v después=%d", offered, after)
	}
}
