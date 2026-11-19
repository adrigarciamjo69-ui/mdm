// Package policy es el motor de políticas del agente: descarga la política
// efectiva, evalúa cada ajuste (y lo aplica en modo «enforce») e informa del
// resultado al servidor.
package policy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"mdm/internal/agent/settings"
	"mdm/internal/protocol"
)

const (
	defaultSettingTimeout = 5 * time.Minute
	maxMessage            = 1024
)

type API interface {
	GetPolicy(ctx context.Context) (protocol.EffectivePolicy, error)
	SendCompliance(ctx context.Context, r protocol.ComplianceReport) error
	SendRecoveryKey(ctx context.Context, k protocol.RecoveryKey) error
}

type Deps struct {
	API API
	// Build crea el ajuste; escrowed son los protectores de BitLocker ya custodiados.
	Build func(s protocol.PolicySetting, escrowed []string) (settings.Setting, error)
	// SettingTimeout es el máximo por ajuste (los scripts propios llevan el suyo).
	SettingTimeout time.Duration
	Now            func() time.Time
	// After permite sustituir los temporizadores en las pruebas.
	After func(d time.Duration) <-chan time.Time
	Log   *slog.Logger
	// OnPolicy recibe cada política nueva descargada (para el software y los parches).
	OnPolicy func(protocol.EffectivePolicy)
}

type Engine struct {
	d       Deps
	trigger chan struct{}

	mu     sync.Mutex
	wanted string
	policy protocol.EffectivePolicy
	have   bool
}

func New(d Deps) *Engine {
	if d.SettingTimeout <= 0 {
		d.SettingTimeout = defaultSettingTimeout
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.After == nil {
		d.After = time.After
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Engine{d: d, trigger: make(chan struct{}, 1)}
}

// Hash devuelve la huella de la política evaluada (la que envía el check-in).
func (e *Engine) Hash() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.policy.Hash
}

// Policy devuelve la última política descargada.
func (e *Engine) Policy() protocol.EffectivePolicy {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.policy
}

// Wanted recibe la huella vigente del servidor tras cada check-in.
func (e *Engine) Wanted(hash string) {
	e.mu.Lock()
	changed := hash != "" && hash != e.wanted
	e.wanted = hash
	differs := hash != e.policy.Hash
	e.mu.Unlock()
	if changed && differs {
		e.Trigger()
	}
}

// Trigger pide una evaluación inmediata (orden «reevaluar políticas»).
func (e *Engine) Trigger() {
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}

func (e *Engine) pending() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.wanted != "" && e.wanted != e.policy.Hash
}

// Run evalúa al cambiar la huella, al pedirlo una orden y cada intervalo.
func (e *Engine) Run(ctx context.Context) {
	for {
		e.mu.Lock()
		interval := time.Duration(e.policy.EvaluateIntervalS) * time.Second
		e.mu.Unlock()
		if interval <= 0 {
			interval = protocol.DefaultEvaluateIntervalS * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-e.trigger:
		case <-e.d.After(interval):
		}
		e.Evaluate(ctx)
	}
}

// Evaluate descarga la política si hace falta, evalúa cada ajuste e informa.
func (e *Engine) Evaluate(ctx context.Context) {
	if e.pending() {
		p, err := e.d.API.GetPolicy(ctx)
		if err != nil {
			e.d.Log.Warn("no se pudo descargar la política; se evalúa la anterior", "err", err)
		} else {
			e.mu.Lock()
			e.policy, e.have = p, true
			e.mu.Unlock()
			if e.d.OnPolicy != nil {
				e.d.OnPolicy(p)
			}
		}
	}
	e.mu.Lock()
	p, have := e.policy, e.have
	e.mu.Unlock()
	if !have {
		return
	}
	report := protocol.ComplianceReport{PolicyHash: p.Hash, Results: []protocol.SettingResult{}}
	for _, s := range p.Settings {
		report.Results = append(report.Results, e.evaluateOne(ctx, s, p.EscrowedKeyIDs))
	}
	report.EvaluatedAt = e.d.Now().UTC()
	if err := e.d.API.SendCompliance(ctx, report); err != nil {
		e.d.Log.Warn("no se pudo enviar el informe de cumplimiento", "err", err)
	}
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxMessage {
		return s
	}
	return string([]rune(s)[:maxMessage])
}

type checkResult struct {
	ok     bool
	detail string
	err    error
}

// withTimeout ejecuta f con el tiempo máximo de un ajuste.
func (e *Engine) withTimeout(ctx context.Context, f func(context.Context) checkResult) checkResult {
	c, cancel := context.WithTimeout(ctx, e.d.SettingTimeout)
	defer cancel()
	done := make(chan checkResult, 1)
	go func() { done <- f(c) }()
	select {
	case r := <-done:
		return r
	case <-c.Done():
		return checkResult{err: errTimeout}
	}
}

var errTimeout = errors.New("Tiempo agotado.")

func (e *Engine) evaluateOne(ctx context.Context, ps protocol.PolicySetting, escrowed []string) protocol.SettingResult {
	res := protocol.SettingResult{PolicyID: ps.PolicyID, Key: ps.Key}
	fail := func(err error) protocol.SettingResult {
		res.Status, res.Message = protocol.StatusError, truncate(err.Error())
		return res
	}
	s, err := e.d.Build(ps, escrowed)
	if err != nil {
		return fail(err)
	}
	check := func() checkResult {
		return e.withTimeout(ctx, func(c context.Context) checkResult {
			ok, detail, err := s.Check(c)
			return checkResult{ok, detail, err}
		})
	}
	r := check()
	if errors.Is(r.err, settings.ErrNotApplicable) {
		res.Status, res.Message = protocol.StatusNotApplicable, truncate(r.detail)
		return res
	}
	if r.err != nil {
		return fail(r.err)
	}
	if !r.ok && ps.Mode == protocol.ModeEnforce {
		ar := e.withTimeout(ctx, func(c context.Context) checkResult { return checkResult{err: s.Apply(c)} })
		if ar.err != nil {
			return fail(ar.err)
		}
		res.Applied = true
		e.d.Log.Info("ajuste aplicado", "key", ps.Key)
		if r = check(); r.err != nil {
			return fail(r.err)
		}
	}
	if rr, ok := s.(settings.RecoveryReporter); ok {
		if k := rr.Recovery(); k != nil {
			if err := e.d.API.SendRecoveryKey(ctx, *k); err != nil {
				e.d.Log.Warn("no se pudo custodiar la clave de recuperación", "err", err)
			}
		}
	}
	res.Message = truncate(r.detail)
	if r.ok {
		res.Status = protocol.StatusCompliant
	} else {
		res.Status = protocol.StatusNoncompliant
	}
	return res
}
