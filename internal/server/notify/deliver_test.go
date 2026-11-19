package notify

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func setupDeliverer(t *testing.T, status *atomic.Int32) (*Deliverer, *store.Store, store.Channel, *time.Time) {
	s := storetest.New(t)
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	box, _ := secrets.New(base64.StdEncoding.EncodeToString(k))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(status.Load())) }))
	t.Cleanup(srv.Close)
	sealed, _ := box.Seal([]byte("secreto"))
	ch, err := s.CreateChannel(ctx, store.Channel{Name: "hook", Type: "webhook", URL: srv.URL, SealedSecret: sealed})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return &Deliverer{Store: s, Secrets: box, Version: "t", HTTP: srv.Client(), Now: func() time.Time { return now }}, s, ch, &now
}

// claim toma la siguiente tarea notify.deliver que ya toca y la procesa.
func claim(t *testing.T, d *Deliverer, s *store.Store, at time.Time) bool {
	t.Helper()
	j, err := s.ClaimJob(ctx, at, time.Minute)
	if err != nil || j == nil {
		return false
	}
	if j.Kind != "notify.deliver" {
		t.Fatalf("tarea inesperada %s", j.Kind)
	}
	if err := d.Handle(ctx, j.Payload); err != nil {
		t.Fatal(err)
	}
	_ = s.FinishJob(ctx, j.ID, at)
	return true
}

func TestDeliverer_RetriesThenSucceeds(t *testing.T) {
	var status atomic.Int32
	status.Store(503)
	d, s, ch, now := setupDeliverer(t, &status)
	if err := Queue(ctx, s.Pool(), ch.ID, Message{Event: "alert.opened", Payload: map[string]any{"x": 1}}, nil, nil, *now); err != nil {
		t.Fatal(err)
	}
	claim(t, d, s, *now)
	if claim(t, d, s, now.Add(59*time.Second)) {
		t.Fatal("el reintento es al minuto")
	}
	status.Store(200)
	*now = now.Add(time.Minute)
	if !claim(t, d, s, *now) {
		t.Fatal("falta el reintento")
	}
	var st string
	var attempts int
	_ = s.Pool().QueryRow(ctx, `SELECT status, attempts FROM deliveries`).Scan(&st, &attempts)
	if st != "sent" || attempts != 2 {
		t.Fatalf("%s %d", st, attempts)
	}
}

func TestDeliverer_GivesUpAndMarksChannel(t *testing.T) {
	var status atomic.Int32
	status.Store(500)
	d, s, ch, now := setupDeliverer(t, &status)
	_ = Queue(ctx, s.Pool(), ch.ID, Message{Event: "alert.opened", Payload: map[string]any{}}, nil, nil, *now)
	at := *now
	for i := 0; i < 6; i++ {
		*now = at
		if !claim(t, d, s, at) {
			t.Fatalf("intento %d no programado", i+1)
		}
		at = at.Add(5 * time.Hour)
	}
	if claim(t, d, s, at.Add(24*time.Hour)) {
		t.Fatal("tras 6 intentos no hay más")
	}
	var st string
	_ = s.Pool().QueryRow(ctx, `SELECT status FROM deliveries`).Scan(&st)
	c, _ := s.GetChannel(ctx, ch.ID)
	if st != "failed" || c.LastError == "" {
		t.Fatalf("%s %q", st, c.LastError)
	}
	_ = json.Marshal
}
