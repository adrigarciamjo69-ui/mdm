package agentapi

import (
	"testing"
	"time"

	"mdm/internal/protocol"
)

func scan(updates ...protocol.UpdateInfo) protocol.UpdateScan {
	return protocol.UpdateScan{ScannedAt: time.Now(), Updates: updates}
}

func TestUpdatesScan_ReplacesPending(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	pub := time.Now().Add(-10 * 24 * time.Hour)
	first := scan(protocol.UpdateInfo{ID: "u1", KB: "KB1", Title: "Acumulativa", Classification: "security", Security: true, PublishedAt: &pub},
		protocol.UpdateInfo{ID: "u2", KB: "KB2", Title: "Defender", Classification: "definition"})
	first.RebootPending = true
	if resp := a.post("/updates", "", c.authz, first, false); resp.StatusCode != 204 {
		t.Fatalf("%d %s", resp.StatusCode, bodyString(resp))
	}
	rows, st, _ := a.store.DeviceUpdates(ctx, c.id)
	if len(rows) != 2 || st.LastScanAt == nil || !st.RebootPending {
		t.Fatalf("tras la primera búsqueda: %+v %+v", rows, st)
	}
	firstSeen := rows[0].FirstSeenAt
	a.post("/updates", "", c.authz, scan(protocol.UpdateInfo{ID: "u1", KB: "KB1", Title: "Acumulativa", Classification: "security", Security: true}), false)
	rows, _, _ = a.store.DeviceUpdates(ctx, c.id)
	var pending []string
	for _, r := range rows {
		if r.State == "missing" {
			pending = append(pending, r.ID)
			if !r.FirstSeenAt.Equal(firstSeen) {
				t.Fatalf("debe conservarse la primera vez que se vio: %v", r.FirstSeenAt)
			}
		} else if r.ID != "u2" || r.State != "installed" {
			t.Fatalf("la que ya no aparece pasa a instalada: %+v", r)
		}
	}
	if len(pending) != 1 || pending[0] != "u1" {
		t.Fatalf("la búsqueda nueva sustituye la lista de pendientes: %v", pending)
	}
	big := protocol.UpdateScan{}
	for i := 0; i < 20000; i++ {
		big.Updates = append(big.Updates, protocol.UpdateInfo{ID: "x", Title: "una actualización con un título bastante largo"})
	}
	wantError(t, a.post("/updates", "", c.authz, big, false), 413, "too_large")
}

func TestUpdatesResults_MarksInstalled(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	a.post("/updates", "", c.authz, scan(protocol.UpdateInfo{ID: "u1", Classification: "security"}, protocol.UpdateInfo{ID: "u2", Classification: "security"}), false)
	res := protocol.UpdateResults{Results: []protocol.UpdateResult{{ID: "u1", Status: "installed"}, {ID: "u2", Status: "failed", Error: "0x80070005"}}, RebootPending: true}
	if resp := a.post("/updates/results", "", c.authz, res, false); resp.StatusCode != 204 {
		t.Fatalf("%d", resp.StatusCode)
	}
	rows, st, _ := a.store.DeviceUpdates(ctx, c.id)
	states := map[string]string{}
	for _, r := range rows {
		states[r.ID] = r.State + "|" + r.LastError
	}
	if states["u1"] != "installed|" || states["u2"] != "failed|0x80070005" || st.LastInstallAt == nil || !st.RebootPending {
		t.Fatalf("%v %+v", states, st)
	}
	wantError(t, a.post("/updates/results", "", c.authz, protocol.UpdateResults{Results: []protocol.UpdateResult{{ID: "u1", Status: "quizá"}}}, false), 400, "invalid_request")
}

// El agente envía la búsqueda comprimida con gzip, como el inventario.
func TestUpdatesScan_AcceptsGzip(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	resp := a.post("/updates", "", c.authz, scan(protocol.UpdateInfo{ID: "openssl", Classification: "security", Security: true}), true)
	if resp.StatusCode != 204 {
		t.Fatalf("búsqueda comprimida: %d %s", resp.StatusCode, bodyString(resp))
	}
	rows, _, _ := a.store.DeviceUpdates(ctx, c.id)
	if len(rows) != 1 {
		t.Fatalf("%+v", rows)
	}
}
