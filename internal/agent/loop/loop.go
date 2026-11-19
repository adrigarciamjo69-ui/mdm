// Package loop es el bucle principal del agente: check-in periódico e
// inventario cuando el servidor lo pide.
package loop

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"mdm/internal/agent/client"
	"mdm/internal/agent/config"
	"mdm/internal/agent/inventory"
	"mdm/internal/protocol"
)

// API es la parte del cliente que usa el bucle.
type API interface {
	Checkin(ctx context.Context, req protocol.CheckinRequest) (protocol.CheckinResponse, error)
	SendInventory(ctx context.Context, inv protocol.Inventory) error
}

type Deps struct {
	API       API
	Collector inventory.Collector
	Config    config.Config
	// State es el estado guardado por la ejecución anterior, para que un
	// reinicio no borre la fecha de la última conexión correcta.
	State      config.State
	SaveConfig func(config.Config) error
	SaveState  func(config.State) error
	// Sleep espera d o hasta que se cancele el contexto, en cuyo caso devuelve su error.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand devuelve un número en [0, 1).
	Rand    func() float64
	Now     func() time.Time
	Version string
	Log     *slog.Logger
	// Capabilities son las funciones que esta versión del agente declara.
	Capabilities []string
	// Commands recibe las órdenes de cada check-in; no debe bloquear.
	Commands func([]protocol.Command)
	// ForceInventory, si devuelve verdadero, envía el inventario en este
	// ciclo aunque el servidor no lo pida (orden «actualizar inventario»).
	ForceInventory func() bool
	// PolicyHash devuelve la huella de la política evaluada; PolicyWanted
	// recibe la vigente del servidor tras cada check-in.
	PolicyHash   func() string
	PolicyWanted func(hash string)
	// Update recibe la actualización que ofrece el servidor; AfterCheckin se
	// llama tras cada check-in correcto.
	Update       func(protocol.AgentUpdate)
	AfterCheckin func()
}

const (
	// collectEvery limita la frecuencia con que se recoge el inventario para
	// calcular su huella: es la operación más cara del agente.
	collectEvery = 5 * time.Minute
	// idleWait es lo que duerme un agente revocado entre comprobaciones de cancelación.
	idleWait = time.Hour
	// checkinJitter reparte los check-ins de muchos agentes (±10 %).
	checkinJitter = 0.1
)

type runner struct {
	d       Deps
	cfg     config.Config
	state   config.State
	backoff *client.Backoff
	log     *slog.Logger
	started time.Time

	inv         protocol.Inventory
	collectedAt time.Time
}

// Run ejecuta el bucle hasta que se cancela el contexto. Nunca se detiene por
// un error: los fallos de red se reintentan y un agente revocado queda en
// espera hasta que se le vuelve a inscribir.
func Run(ctx context.Context, d Deps) error {
	r := &runner{d: d, cfg: d.Config, state: d.State, backoff: client.NewBackoff(d.Rand), log: d.Log, started: d.Now()}
	if r.log == nil {
		r.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if r.cfg.CheckinIntervalS <= 0 {
		r.cfg.CheckinIntervalS = protocol.DefaultCheckinIntervalS
	}
	for ctx.Err() == nil {
		wait := idleWait
		if !r.cfg.Revoked {
			wait = r.cycle(ctx)
		}
		if err := d.Sleep(ctx, wait); err != nil {
			break
		}
	}
	return nil
}

// cycle hace un check-in (y envía el inventario si se le pide) y devuelve
// cuánto esperar hasta el siguiente.
func (r *runner) cycle(ctx context.Context) time.Duration {
	now := r.d.Now()
	if r.collectedAt.IsZero() || now.Sub(r.collectedAt) >= collectEvery {
		r.inv = r.d.Collector.Collect(ctx)
		r.collectedAt = now
	}

	resp, err := r.d.API.Checkin(ctx, protocol.CheckinRequest{
		AgentVersion:  r.d.Version,
		UptimeS:       int64(now.Sub(r.started).Seconds()),
		InventoryHash: r.inv.Hash,
		Capabilities:  r.d.Capabilities,
		PolicyHash:    r.policyHash(),
	})
	if err != nil {
		return r.failed(err, "check-in")
	}
	r.backoff.Reset()
	r.applyConfig(resp.Config)
	// resp.Commands se ignora a propósito: en esta fase el agente no ejecuta
	// nada que le ordene el servidor.

	r.state.LastSuccess, r.state.LastError, r.state.LastErrorAt = &now, "", nil
	if r.d.AfterCheckin != nil {
		r.d.AfterCheckin()
	}
	if resp.AgentUpdate != nil && r.d.Update != nil {
		r.d.Update(*resp.AgentUpdate)
	}
	if r.d.PolicyWanted != nil {
		r.d.PolicyWanted(resp.PolicyHash)
	}
	if len(resp.Commands) > 0 && r.d.Commands != nil {
		r.d.Commands(resp.Commands)
	}
	if r.d.ForceInventory != nil && r.d.ForceInventory() {
		r.inv = r.d.Collector.Collect(ctx)
		r.collectedAt = now
		resp.SendInventory = true
	}
	if resp.SendInventory {
		if err := r.d.API.SendInventory(ctx, r.inv); err != nil {
			if errors.Is(err, client.ErrRevoked) {
				return r.failed(err, "inventario")
			}
			// El check-in ha ido bien: un fallo del inventario no altera el
			// ritmo. El servidor lo volverá a pedir en el siguiente ciclo.
			r.noteError(err, "inventario")
		} else {
			r.log.Info("inventario enviado", "programas", len(r.inv.Software))
		}
	}
	r.saveState()
	return r.interval()
}

// failed anota el error y decide la espera: la que pida el servidor, la
// espera creciente, o ninguna llamada más si el equipo está revocado.
func (r *runner) failed(err error, what string) time.Duration {
	r.noteError(err, what)
	r.saveState()
	var retry *client.RetryAfterError
	switch {
	case errors.Is(err, client.ErrRevoked):
		r.cfg.Revoked = true
		r.log.Warn("el servidor ha revocado este equipo: el agente queda en espera hasta que se vuelva a inscribir")
		if err := r.d.SaveConfig(r.cfg); err != nil {
			r.log.Error("no se pudo guardar la configuración", "err", err)
		}
		return idleWait
	case errors.As(err, &retry):
		return retry.After
	}
	return r.backoff.Next()
}

func (r *runner) noteError(err error, what string) {
	now := r.d.Now()
	r.state.LastError, r.state.LastErrorAt = what+": "+err.Error(), &now
	r.log.Warn("fallo de comunicación", "en", what, "err", err)
}

func (r *runner) saveState() {
	if err := r.d.SaveState(r.state); err != nil {
		r.log.Error("no se pudo guardar el estado", "err", err)
	}
}

// applyConfig adopta los intervalos que envía el servidor, ignorando valores
// no positivos, y los guarda solo si cambian.
func (r *runner) applyConfig(c protocol.AgentConfig) {
	changed := false
	if c.CheckinIntervalS > 0 && c.CheckinIntervalS != r.cfg.CheckinIntervalS {
		r.cfg.CheckinIntervalS, changed = c.CheckinIntervalS, true
	}
	if c.InventoryIntervalS > 0 && c.InventoryIntervalS != r.cfg.InventoryIntervalS {
		r.cfg.InventoryIntervalS, changed = c.InventoryIntervalS, true
	}
	if changed {
		if err := r.d.SaveConfig(r.cfg); err != nil {
			r.log.Error("no se pudo guardar la configuración", "err", err)
		}
	}
}

func (r *runner) policyHash() string {
	if r.d.PolicyHash == nil {
		return ""
	}
	return r.d.PolicyHash()
}

// interval devuelve el intervalo de check-in con una variación de ±10 %.
func (r *runner) interval() time.Duration {
	base := time.Duration(r.cfg.CheckinIntervalS) * time.Second
	factor := 1 - checkinJitter + 2*checkinJitter*r.d.Rand()
	return time.Duration(float64(base) * factor)
}
