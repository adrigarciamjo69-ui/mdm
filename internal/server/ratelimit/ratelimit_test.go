package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest() (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)}
	return New(5, 15*time.Minute, c.now), c
}

func TestLimiter_BlocksAfterLimit(t *testing.T) {
	l, _ := newTest()
	for i := 0; i < 4; i++ {
		l.Record("k")
	}
	if b, _ := l.Blocked("k"); b {
		t.Fatal("bloqueado con 4 registros")
	}
	l.Record("k")
	if b, _ := l.Blocked("k"); !b {
		t.Fatal("no bloqueado con 5 registros")
	}
}

func TestLimiter_WindowExpires(t *testing.T) {
	l, c := newTest()
	for i := 0; i < 5; i++ {
		l.Record("k")
	}
	c.t = c.t.Add(15*time.Minute + time.Second)
	if b, _ := l.Blocked("k"); b {
		t.Fatal("sigue bloqueado tras caducar la ventana")
	}
}

func TestLimiter_RetryAfter(t *testing.T) {
	l, c := newTest()
	for i := 0; i < 5; i++ {
		l.Record("k")
	}
	c.t = c.t.Add(5 * time.Minute)
	b, wait := l.Blocked("k")
	if !b || wait != 10*time.Minute {
		t.Fatalf("blocked=%v wait=%v", b, wait)
	}
}

func TestLimiter_KeysIndependent(t *testing.T) {
	l, _ := newTest()
	for i := 0; i < 5; i++ {
		l.Record("a")
	}
	if b, _ := l.Blocked("b"); b {
		t.Fatal("la clave b quedó bloqueada por la clave a")
	}
}

func TestLimiter_Reset(t *testing.T) {
	l, _ := newTest()
	for i := 0; i < 5; i++ {
		l.Record("k")
	}
	l.Reset("k")
	if b, _ := l.Blocked("k"); b {
		t.Fatal("sigue bloqueado tras Reset")
	}
}

func TestLimiter_ForgetsIdleKeys(t *testing.T) {
	l, c := newTest()
	for i := 0; i < 1000; i++ {
		l.Record(string(rune('a'+i%26)) + time.Duration(i).String())
	}
	c.t = c.t.Add(16 * time.Minute)
	l.Record("otra")
	if n := l.size(); n > 1 {
		t.Fatalf("quedan %d claves caducadas en memoria", n)
	}
}
