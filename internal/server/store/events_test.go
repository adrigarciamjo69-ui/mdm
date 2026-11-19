package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

// queued devuelve los sucesos de la cola, sin los de inscripción (que produce la siembra).
func queued(t *testing.T, s *store.Store) []store.Event {
	var out []store.Event
	for _, e := range allQueued(t, s) {
		if e.Type != store.EventDeviceEnrolled {
			out = append(out, e)
		}
	}
	return out
}

func allQueued(t *testing.T, s *store.Store) []store.Event {
	t.Helper()
	rows, err := s.Pool().Query(context.Background(), `SELECT payload FROM jobs WHERE kind = 'events.dispatch' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.Event
	for rows.Next() {
		var raw []byte
		_ = rows.Scan(&raw)
		var e store.Event
		_ = json.Unmarshal(raw, &e)
		out = append(out, e)
	}
	return out
}

func TestEvents_ComplianceChangedOnlyOnChange(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	id := storetest.SeedDevice(t, s, "pc-1", "windows")
	now := time.Now()
	_, _ = s.SaveComplianceState(ctx, id, "compliant", nil, nil, now)
	_, _ = s.SaveComplianceState(ctx, id, "noncompliant", []string{"x"}, &now, now)
	_, _ = s.SaveComplianceState(ctx, id, "noncompliant", []string{"x"}, &now, now)
	ev := queued(t, s)
	if len(ev) != 2 || ev[1].Type != store.EventComplianceChanged || ev[1].Data["to"] != "noncompliant" || ev[1].Data["from"] != "compliant" || *ev[1].DeviceID != id {
		t.Fatalf("%+v", ev)
	}
}

func TestEvents_DeploymentAndCommandFailed(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	id := storetest.SeedDevice(t, s, "pc-1", "windows")
	dep := uuid.New()
	_, _ = s.Pool().Exec(ctx, `INSERT INTO apps (id, name, platform, source, package_id) VALUES ($1, 'Firefox', 'windows', 'winget', 'Mozilla.Firefox')`, dep)
	_, _ = s.Pool().Exec(ctx, `INSERT INTO deployments (id, app_id, intent) VALUES ($1, $1, 'required')`, dep)
	st := protocol.AppStatus{DeploymentID: dep.String(), State: "pending", UpdatedAt: time.Now()}
	_ = s.SaveAppStatus(ctx, id, []protocol.AppStatus{st})
	st.State, st.Message = "failed", "código 1603"
	_ = s.SaveAppStatus(ctx, id, []protocol.AppStatus{st})
	_ = s.SaveAppStatus(ctx, id, []protocol.AppStatus{st}) // sigue fallido: no se repite
	ev := queued(t, s)
	if len(ev) != 1 || ev[0].Type != store.EventDeploymentFailed || ev[0].Data["deployment_id"] != dep.String() {
		t.Fatalf("%+v", ev)
	}
}

func TestEvents_EnrollAndPublish(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	storetest.SeedDevice(t, s, "pc-1", "linux") // se inscribe con EnrollDevice
	ev := allQueued(t, s)
	if len(ev) != 1 || ev[0].Type != store.EventDeviceEnrolled {
		t.Fatalf("%+v", ev)
	}
	if err := s.PublishEvent(ctx, store.Event{Type: store.EventLoginFailed, Data: map[string]any{"email": "x@y"}}); err != nil {
		t.Fatal(err)
	}
	if ev := allQueued(t, s); len(ev) != 2 || ev[1].DeviceID != nil {
		t.Fatalf("%+v", ev)
	}
}
