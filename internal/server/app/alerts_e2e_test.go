package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/alerts"
	"mdm/internal/server/jobs"
	"mdm/internal/server/notify"
	"mdm/internal/server/policy"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

// Cadena completa: inscripción → suceso en la cola → alerta → aviso a los 2
// minutos → entrega firmada al webhook.
func TestAlertChain_EnrollToWebhook(t *testing.T) {
	s := storetest.New(t)
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	box, _ := secrets.New(base64.StdEncoding.EncodeToString(k))
	var mu sync.Mutex
	var bodies []string
	var sigOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("secreto-del-canal"))
		mac.Write(b)
		mu.Lock()
		bodies = append(bodies, string(b))
		sigOK = r.Header.Get("X-MDM-Signature") == "sha256="+hex.EncodeToString(mac.Sum(nil))
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	sealed, _ := box.Seal([]byte("secreto-del-canal"))
	ch, _ := s.CreateChannel(ctx, store.Channel{Name: "hook", Type: "webhook", URL: srv.URL, SealedSecret: sealed})
	_, err := s.CreateAlertRule(ctx, store.AlertRule{Name: "Equipos nuevos", Type: alerts.TypeDeviceEnrolled, Params: json.RawMessage(`{}`),
		Severity: "info", Channels: []uuid.UUID{ch.ID}, Enabled: true}, []policy.Assignment{{}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	runner := jobs.New(s, clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine := alerts.New(alerts.Deps{Store: s, Now: clock, PublicURL: "https://mdm.ejemplo.es"})
	RegisterAlertJobs(runner, engine, &notify.Deliverer{Store: s, Secrets: box, Version: "t", HTTP: srv.Client(), Now: clock})

	storetest.SeedDevice(t, s, "PC-NUEVO", "windows") // publica device.enrolled
	now = time.Now().UTC().Add(time.Second)
	drain := func() {
		for i := 0; i < 20; i++ {
			if ok, _ := runner.RunOnce(ctx); !ok {
				return
			}
		}
	}
	drain()
	list, _, _ := s.ListAlerts(ctx, store.AlertFilter{State: "unresolved", Page: 1, PerPage: 10})
	if len(list) != 1 || !strings.Contains(list[0].Title, "PC-NUEVO") {
		t.Fatalf("alerta: %+v", list)
	}
	if len(bodies) != 0 {
		t.Fatal("el aviso espera 2 minutos para agrupar")
	}
	now = now.Add(2*time.Minute + time.Second)
	drain()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !sigOK || !strings.Contains(bodies[0], `"event":"alert.opened"`) || !strings.Contains(bodies[0], "PC-NUEVO") ||
		!strings.Contains(bodies[0], "https://mdm.ejemplo.es/alertas/") {
		t.Fatalf("entrega: %v firma=%v", bodies, sigOK)
	}
}
