package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func setup(t *testing.T) (*store.Store, *Runner, *clock) {
	s := storetest.New(t)
	c := &clock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	return s, New(s, c.now, nil), c
}

func enqueue(t *testing.T, s *store.Store, j store.NewJob) {
	t.Helper()
	if err := store.EnqueueJob(ctx, s.Pool(), j); err != nil {
		t.Fatal(err)
	}
}

func TestRunner_ExecutesAndFinishes(t *testing.T) {
	s, r, c := setup(t)
	var got string
	r.Handle("saludo", func(_ context.Context, p json.RawMessage) error {
		var v struct{ Nombre string }
		_ = json.Unmarshal(p, &v)
		got = v.Nombre
		return nil
	})
	enqueue(t, s, store.NewJob{Kind: "saludo", Payload: map[string]string{"Nombre": "Ana"}, RunAt: c.now()})
	ran, err := r.RunOnce(ctx)
	if err != nil || !ran || got != "Ana" {
		t.Fatalf("ran=%v err=%v got=%q", ran, err, got)
	}
	if ran, _ := r.RunOnce(ctx); ran {
		t.Fatal("la tarea se ejecutó dos veces")
	}
	jobs, _, _ := s.ListJobs(ctx, "pending", 1, 10)
	if len(jobs) != 0 {
		t.Fatalf("quedan pendientes: %+v", jobs)
	}
}

func TestRunner_FutureJobWaits(t *testing.T) {
	s, r, c := setup(t)
	r.Handle("x", func(context.Context, json.RawMessage) error { return nil })
	enqueue(t, s, store.NewJob{Kind: "x", RunAt: c.now().Add(time.Minute)})
	if ran, _ := r.RunOnce(ctx); ran {
		t.Fatal("se ejecutó antes de tiempo")
	}
	c.add(time.Minute)
	if ran, _ := r.RunOnce(ctx); !ran {
		t.Fatal("no se ejecutó a su hora")
	}
}

func TestRunner_RetriesWithBackoff(t *testing.T) {
	s, r, c := setup(t)
	calls := 0
	r.Handle("falla", func(context.Context, json.RawMessage) error { calls++; return errors.New("boom") })
	enqueue(t, s, store.NewJob{Kind: "falla", RunAt: c.now()})
	for i, wait := range []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute, 32 * time.Minute} {
		if ran, _ := r.RunOnce(ctx); !ran {
			t.Fatalf("intento %d no se ejecutó", i+1)
		}
		c.add(wait - time.Second)
		if ran, _ := r.RunOnce(ctx); ran {
			t.Fatalf("intento %d: reintento antes de %v", i+1, wait)
		}
		c.add(time.Second)
	}
	if ran, _ := r.RunOnce(ctx); !ran { // quinto intento
		t.Fatal("quinto intento no se ejecutó")
	}
	c.add(24 * time.Hour)
	if ran, _ := r.RunOnce(ctx); ran || calls != 5 {
		t.Fatalf("tras 5 intentos no debe reintentar: llamadas %d", calls)
	}
	failed, n, _ := s.ListJobs(ctx, "failed", 1, 10)
	if n != 1 || failed[0].LastError != "boom" || failed[0].Attempts != 5 {
		t.Fatalf("fallidas: %+v", failed)
	}
}

func TestRunner_NoTwoWorkersSameJob(t *testing.T) {
	s, _, c := setup(t)
	var counts sync.Map
	var total atomic.Int64
	handler := func(_ context.Context, p json.RawMessage) error {
		var v struct{ N int }
		_ = json.Unmarshal(p, &v)
		n, _ := counts.LoadOrStore(v.N, new(atomic.Int64))
		n.(*atomic.Int64).Add(1)
		total.Add(1)
		return nil
	}
	for i := 0; i < 50; i++ {
		enqueue(t, s, store.NewJob{Kind: "w", Payload: map[string]int{"N": i}, RunAt: c.now()})
	}
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		r := New(s, c.now, nil)
		r.Handle("w", handler)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				ran, err := r.RunOnce(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if !ran {
					return
				}
			}
		}()
	}
	wg.Wait()
	if total.Load() != 50 {
		t.Fatalf("ejecuciones: %d", total.Load())
	}
	counts.Range(func(k, v any) bool {
		if v.(*atomic.Int64).Load() != 1 {
			t.Errorf("tarea %v ejecutada %d veces", k, v.(*atomic.Int64).Load())
		}
		return true
	})
}

func TestRunner_UniqueKeyDedupes(t *testing.T) {
	s, r, c := setup(t)
	r.Handle("u", func(context.Context, json.RawMessage) error { return nil })
	enqueue(t, s, store.NewJob{Kind: "u", RunAt: c.now(), UniqueKey: "k"})
	enqueue(t, s, store.NewJob{Kind: "u", RunAt: c.now(), UniqueKey: "k"})
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 1 {
		t.Fatalf("pendientes: %d", n)
	}
	r.RunOnce(ctx)
	enqueue(t, s, store.NewJob{Kind: "u", RunAt: c.now(), UniqueKey: "k"})
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 1 {
		t.Fatalf("tras terminar debe poder encolarse de nuevo: %d", n)
	}
}

func TestRunner_Every(t *testing.T) {
	s, r, c := setup(t)
	calls := 0
	r.Handle("tic", func(context.Context, json.RawMessage) error { calls++; return nil })
	r.Every("tic", time.Hour)
	if err := r.SchedulePeriodic(ctx); err != nil {
		t.Fatal(err)
	}
	r.RunOnce(ctx)
	if calls != 1 {
		t.Fatalf("primera ejecución inmediata: %d", calls)
	}
	pending, _, _ := s.ListJobs(ctx, "pending", 1, 10)
	if len(pending) != 1 || !pending[0].RunAt.Equal(c.now().Add(time.Hour)) {
		t.Fatalf("siguiente: %+v", pending)
	}
	// Reiniciar el servidor no duplica la periódica.
	r2 := New(s, c.now, nil)
	r2.Handle("tic", func(context.Context, json.RawMessage) error { return nil })
	r2.Every("tic", time.Hour)
	_ = r2.SchedulePeriodic(ctx)
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 1 {
		t.Fatalf("duplicada tras reinicio: %d", n)
	}
}

func TestRunner_PeriodicKeepsGoingAfterFinalFailure(t *testing.T) {
	s, r, c := setup(t)
	r.Handle("p", func(context.Context, json.RawMessage) error { return errors.New("siempre") })
	r.Every("p", time.Hour)
	_ = r.SchedulePeriodic(ctx)
	for i := 0; i < 5; i++ {
		r.RunOnce(ctx)
		c.add(time.Hour)
	}
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 1 {
		t.Fatalf("la periódica debe seguir programada tras fallar del todo: %d", n)
	}
}

func TestRunner_LockExpiredIsRetaken(t *testing.T) {
	s, r, c := setup(t)
	enqueue(t, s, store.NewJob{Kind: "l", RunAt: c.now()})
	if _, err := s.ClaimJob(ctx, c.now(), 5*time.Minute); err != nil { // un trabajador que «muere» con ella tomada
		t.Fatal(err)
	}
	r.Handle("l", func(context.Context, json.RawMessage) error { return nil })
	if ran, _ := r.RunOnce(ctx); ran {
		t.Fatal("se tomó una tarea bloqueada")
	}
	c.add(5*time.Minute + time.Second)
	if ran, _ := r.RunOnce(ctx); !ran {
		t.Fatal("no se retomó tras caducar el bloqueo")
	}
}

func TestRunner_UnknownKindFails(t *testing.T) {
	s, r, c := setup(t)
	enqueue(t, s, store.NewJob{Kind: "desconocida", RunAt: c.now()})
	r.RunOnce(ctx)
	failed, _, _ := s.ListJobs(ctx, "failed", 1, 10)
	if len(failed) != 1 || failed[0].LastError != "tipo de tarea desconocido: desconocida" {
		t.Fatalf("fallidas: %+v", failed)
	}
}

func TestRunner_PanicIsFailure(t *testing.T) {
	s, r, c := setup(t)
	r.Handle("pan", func(context.Context, json.RawMessage) error { panic("ay") })
	enqueue(t, s, store.NewJob{Kind: "pan", RunAt: c.now(), MaxAttempts: 1})
	if _, err := r.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	failed, _, _ := s.ListJobs(ctx, "failed", 1, 10)
	if len(failed) != 1 || failed[0].LastError == "" {
		t.Fatalf("fallidas: %+v", failed)
	}
}

func TestEnqueueInsideTransaction(t *testing.T) {
	s, _, c := setup(t)
	err := s.Tx(ctx, func(q store.Querier) error {
		if err := store.EnqueueJob(ctx, q, store.NewJob{Kind: "x", RunAt: c.now()}); err != nil {
			return err
		}
		return fmt.Errorf("deshacer")
	})
	if err == nil {
		t.Fatal("se esperaba el error de la transacción")
	}
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 0 {
		t.Fatalf("la tarea sobrevivió al rollback: %d", n)
	}
}
