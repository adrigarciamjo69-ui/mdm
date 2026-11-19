package client

import "time"

const (
	backoffBase   = 5 * time.Second
	backoffMax    = 15 * time.Minute
	backoffJitter = 0.2
)

// Backoff calcula esperas crecientes entre reintentos: 5 s, 10 s, 20 s… hasta
// 15 minutos, con una variación aleatoria de ±20 % para que muchos agentes no
// reintenten a la vez.
type Backoff struct {
	rnd     func() float64
	current time.Duration
}

// NewBackoff recibe una fuente de números aleatorios en [0, 1).
func NewBackoff(rnd func() float64) *Backoff {
	return &Backoff{rnd: rnd}
}

// Next devuelve la siguiente espera y avanza la secuencia.
func (b *Backoff) Next() time.Duration {
	if b.current == 0 {
		b.current = backoffBase
	} else if b.current < backoffMax {
		b.current *= 2
		if b.current > backoffMax {
			b.current = backoffMax
		}
	}
	factor := 1 - backoffJitter + 2*backoffJitter*b.rnd()
	return time.Duration(float64(b.current) * factor)
}

// Reset vuelve al principio de la secuencia tras un éxito.
func (b *Backoff) Reset() { b.current = 0 }
