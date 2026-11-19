package agentapi

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

func TestCheckin_UpdatesLastSeenAndActivity(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	a.clock.Advance(10 * time.Minute)
	resp, _ := c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.1", UptimeS: 100})
	if resp.StatusCode != 200 {
		t.Fatalf("estado %d: %s", resp.StatusCode, bodyString(resp))
	}
	d := a.device(c.id.String())
	if d.LastSeenAt == nil || !d.LastSeenAt.Equal(a.clock.Now()) || d.LastIP != "203.0.113.50" || d.AgentVersion != "1.0.1" {
		t.Fatalf("dispositivo: last_seen=%v ip=%s versión=%s", d.LastSeenAt, d.LastIP, d.AgentVersion)
	}
	act, err := a.store.DeviceActivity(ctx, c.id, a.clock.Now().Add(-time.Hour))
	if err != nil || len(act) != 1 || act[0].Checkins != 1 || !act[0].Hour.Equal(a.clock.Now().Truncate(time.Hour)) {
		t.Fatalf("actividad: %+v %v", act, err)
	}
	c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.1"})
	act, _ = a.store.DeviceActivity(ctx, c.id, a.clock.Now().Add(-time.Hour))
	if len(act) != 1 || act[0].Checkins != 2 {
		t.Fatalf("segundo check-in: %+v", act)
	}
	a.clock.Advance(time.Hour)
	c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.1"})
	act, _ = a.store.DeviceActivity(ctx, c.id, a.clock.Now().Add(-3*time.Hour))
	if len(act) != 2 || !act[0].Hour.Before(act[1].Hour) {
		t.Fatalf("otra hora: %+v", act)
	}
}

func TestCheckin_RequestsInventoryWhenHashDiffers(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	if _, out := c.checkin(protocol.CheckinRequest{InventoryHash: "sha256:abc"}); !out.SendInventory {
		t.Fatal("un dispositivo sin inventario debe pedirlo")
	}
	inv := sampleInventory()
	c.inventory(inv)
	if _, out := c.checkin(protocol.CheckinRequest{InventoryHash: "sha256:otra"}); !out.SendInventory {
		t.Fatal("una huella distinta debe pedir inventario")
	}
}

func TestCheckin_NoInventoryWhenHashMatchesAndFresh(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	if resp := c.inventory(inv); resp.StatusCode != 204 {
		t.Fatalf("inventario: %d %s", resp.StatusCode, bodyString(resp))
	}
	a.clock.Advance(21599 * time.Second)
	if _, out := c.checkin(protocol.CheckinRequest{InventoryHash: inv.Hash}); out.SendInventory {
		t.Fatal("con la misma huella y un inventario reciente no debe pedirlo")
	}
}

func TestCheckin_RequestsInventoryWhenStale(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	c.inventory(inv)
	a.clock.Advance(21601 * time.Second)
	if _, out := c.checkin(protocol.CheckinRequest{InventoryHash: inv.Hash}); !out.SendInventory {
		t.Fatal("un inventario de más de 6 h debe renovarse")
	}
}

func TestCheckin_ResponseShape(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	resp := a.post("/checkin", "", c.authz, protocol.CheckinRequest{}, false)
	body := bodyString(resp)
	for _, want := range []string{`"checkin_interval_s":60`, `"inventory_interval_s":21600`, `"commands":[]`, `"send_inventory":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("falta %s en %s", want, body)
		}
	}
}

func TestCheckin_Unauthorized(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	otherSecret, _, _ := auth.NewSecret()
	for name, authz := range map[string]string{
		"sin cabecera":     "",
		"secreto erróneo":  "Bearer " + auth.FormatDeviceCredential(c.id.String(), otherSecret),
		"UUID inexistente": "Bearer " + auth.FormatDeviceCredential(uuid.NewString(), otherSecret),
		"formato inválido": "Bearer basura",
		"esquema distinto": strings.Replace(c.authz, "Bearer", "Basic", 1),
	} {
		resp := a.post("/checkin", "", authz, protocol.CheckinRequest{}, false)
		if resp.StatusCode != 401 || !strings.Contains(bodyString(resp), `"code":"unauthorized"`) {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
	if d := a.device(c.id.String()); d.LastSeenAt != nil {
		t.Error("un check-in rechazado actualizó last_seen_at")
	}
}

func TestCheckin_RevokedDevice(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	_ = a.store.RevokeDevice(ctx, c.id, a.clock.Now())
	resp, _ := c.checkin(protocol.CheckinRequest{})
	wantError(t, resp, 401, "unauthorized")
	wantError(t, c.inventory(sampleInventory()), 401, "unauthorized")
}

func TestCheckin_OldCredentialAfterReenroll(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	_, first := a.enroll(sampleEnroll(token, "hw-1"))
	_, second := a.enroll(sampleEnroll(token, "hw-1"))
	old := "Bearer " + auth.FormatDeviceCredential(first.DeviceID, first.Credential)
	wantError(t, a.post("/checkin", "", old, protocol.CheckinRequest{}, false), 401, "unauthorized")
	fresh := "Bearer " + auth.FormatDeviceCredential(second.DeviceID, second.Credential)
	if resp := a.post("/checkin", "", fresh, protocol.CheckinRequest{}, false); resp.StatusCode != 200 {
		t.Fatalf("la credencial nueva debe valer: %d", resp.StatusCode)
	}
}

func TestCheckin_BodyTooLarge(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	big := `{"agent_version":"` + strings.Repeat("x", 17*1024) + `"}`
	wantError(t, a.post("/checkin", "", c.authz, big, false), 413, "too_large")
}
