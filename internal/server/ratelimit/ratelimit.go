// Package ratelimit limita intentos por clave con una ventana deslizante en memoria.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	events    map[string][]time.Time
	lastSweep time.Time
}

// New crea un limitador que bloquea una clave cuando acumula limit registros
// dentro de window.
func New(limit int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{limit: limit, window: window, now: now, events: map[string][]time.Time{}}
}

// Record anota un intento para la clave.
func (l *Limiter) Record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	l.events[key] = append(l.prune(key, now), now)
}

// Blocked informa de si la clave ha alcanzado el límite y, en ese caso, cuánto
// falta para que caduque su registro más antiguo.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ev := l.prune(key, now)
	if len(ev) == 0 {
		delete(l.events, key)
		return false, 0
	}
	l.events[key] = ev
	if len(ev) < l.limit {
		return false, 0
	}
	oldest := ev[len(ev)-l.limit]
	return true, oldest.Add(l.window).Sub(now)
}

// Reset olvida los intentos de la clave.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.events, key)
}

// prune devuelve los registros de la clave que siguen dentro de la ventana.
func (l *Limiter) prune(key string, now time.Time) []time.Time {
	ev := l.events[key]
	cut := now.Add(-l.window)
	i := 0
	for i < len(ev) && !ev[i].After(cut) {
		i++
	}
	return ev[i:]
}

// sweep elimina las claves sin registros vigentes, como mucho una vez por
// ventana, para que la memoria no crezca con claves que nunca vuelven.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for k := range l.events {
		if ev := l.prune(k, now); len(ev) == 0 {
			delete(l.events, k)
		} else {
			l.events[k] = ev
		}
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}
