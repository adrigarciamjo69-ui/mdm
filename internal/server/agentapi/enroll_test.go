package agentapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

func TestEnroll_Success(t *testing.T) {
	a := newAgentAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	plain, tok := a.tokenWith(store.TokenParams{GroupID: &g.ID})
	resp, out := a.enroll(sampleEnroll(plain, "hw-1"))
	if resp.StatusCode != 201 {
		t.Fatalf("estado %d: %s", resp.StatusCode, bodyString(resp))
	}
	if _, err := uuid.Parse(out.DeviceID); err != nil {
		t.Errorf("device_id no es UUID: %q", out.DeviceID)
	}
	if len(out.Credential) != 43 || out.CheckinIntervalS != 60 || out.InventoryIntervalS != 21600 {
		t.Errorf("respuesta: %+v", out)
	}
	d := a.device(out.DeviceID)
	if d.Hostname != "PC-A" || d.Platform != "windows" || d.OSName != "Windows 11 Pro" || d.Serial != "SN123" ||
		d.AgentVersion != "1.0.0" || d.HardwareID != "hw-1" || d.LastIP != "203.0.113.10" {
		t.Errorf("dispositivo: %+v", d)
	}
	if d.GroupID == nil || *d.GroupID != g.ID {
		t.Errorf("no heredó el grupo del token: %v", d.GroupID)
	}
	if d.CredentialHash != auth.HashSecret(out.Credential) {
		t.Error("credential_hash no corresponde a la credencial devuelta")
	}
	if !d.EnrolledAt.Equal(a.clock.Now()) || d.RevokedAt != nil {
		t.Errorf("fechas: %+v", d)
	}
	tokens, _ := a.store.ListTokens(ctx)
	if tokens[0].ID != tok.ID || tokens[0].Uses != 1 {
		t.Errorf("usos del token: %d", tokens[0].Uses)
	}
}

func TestEnroll_InvalidTokensAllLookTheSame(t *testing.T) {
	a := newAgentAPI(t)
	past := a.clock.Now().Add(-time.Second)
	one := 1
	expired := a.token(nil, &past)
	exhausted := a.token(&one, nil)
	if resp, _ := a.enroll(sampleEnroll(exhausted, "hw-ok")); resp.StatusCode != 201 {
		t.Fatal("la primera inscripción debía funcionar")
	}
	revoked, tok := a.tokenWith(store.TokenParams{})
	_ = a.store.RevokeToken(ctx, tok.ID, a.clock.Now())

	var bodies []string
	for i, token := range []string{"no-existe", expired, exhausted, revoked} {
		resp, _ := a.enrollFrom(fmt.Sprintf("198.51.100.%d", i+1), sampleEnroll(token, fmt.Sprintf("hw-%d", i)))
		body := bodyString(resp)
		if resp.StatusCode != 403 || !strings.Contains(body, `"code":"forbidden"`) {
			t.Fatalf("caso %d: %d %s", i, resp.StatusCode, body)
		}
		bodies = append(bodies, body)
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("las respuestas difieren:\n%s\n%s", bodies[0], b)
		}
	}
}

func TestEnroll_MissingFields(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	bad := sampleEnroll(token, "")
	wantError(t, a.post("/enroll", "", "", bad, false), 400, "invalid_request")
	bad = sampleEnroll(token, "hw-1")
	bad.Hostname = "  "
	wantError(t, a.post("/enroll", "", "", bad, false), 400, "invalid_request")
	bad = sampleEnroll(token, "hw-1")
	bad.Platform = "macos"
	wantError(t, a.post("/enroll", "", "", bad, false), 400, "invalid_request")
	bad = sampleEnroll("", "hw-1")
	wantError(t, a.post("/enroll", "", "", bad, false), 400, "invalid_request")
	wantError(t, a.post("/enroll", "", "", "{", false), 400, "invalid_request")
	wantError(t, a.post("/enroll", "", "", `{"token":"`+strings.Repeat("x", 17*1024)+`"}`, false), 413, "too_large")
}

func TestEnroll_FailedAttemptDoesNotConsumeUse(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	bad := sampleEnroll(token, "hw-1")
	bad.Platform = "macos"
	a.post("/enroll", "", "", bad, false)
	tokens, _ := a.store.ListTokens(ctx)
	if tokens[0].Uses != 0 {
		t.Fatalf("una petición inválida consumió un uso: %d", tokens[0].Uses)
	}
}

func TestEnroll_RateLimitCountsOnlyFailures(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	for i := 0; i < 10; i++ {
		resp, _ := a.enrollFrom("198.51.100.1", sampleEnroll("token-malo", fmt.Sprintf("hw-x%d", i)))
		if resp.StatusCode != 403 {
			t.Fatalf("intento %d: %d", i, resp.StatusCode)
		}
	}
	resp, _ := a.enrollFrom("198.51.100.1", sampleEnroll(token, "hw-bloqueado"))
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("undécimo intento: %d Retry-After=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	for i := 0; i < 30; i++ {
		resp, _ := a.enrollFrom("198.51.100.2", sampleEnroll(token, fmt.Sprintf("hw-%d", i)))
		if resp.StatusCode != 201 {
			t.Fatalf("inscripción válida %d desde otra IP: %d %s", i, resp.StatusCode, bodyString(resp))
		}
	}
	a.clock.Advance(time.Minute + time.Second)
	if resp, _ := a.enrollFrom("198.51.100.1", sampleEnroll(token, "hw-bloqueado")); resp.StatusCode != 201 {
		t.Fatalf("tras la ventana debería poder inscribirse: %d", resp.StatusCode)
	}
}

func TestEnroll_ReenrollKeepsRecordAndRotatesCredential(t *testing.T) {
	a := newAgentAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	other, _ := a.store.CreateGroup(ctx, "Otro", "")
	tokenA, _ := a.tokenWith(store.TokenParams{GroupID: &g.ID})
	tokenB, _ := a.tokenWith(store.TokenParams{GroupID: &other.ID})
	_, first := a.enroll(sampleEnroll(tokenA, "hw-1"))
	id := uuid.MustParse(first.DeviceID)
	user, tags := "ana", []string{"vip"}
	if err := a.store.UpdateDevice(ctx, id, store.DevicePatch{AssignedUser: &user, Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RevokeDevice(ctx, id, a.clock.Now()); err != nil {
		t.Fatal(err)
	}
	enrolledAt := a.device(first.DeviceID).EnrolledAt
	a.clock.Advance(time.Hour)

	req := sampleEnroll(tokenB, "hw-1")
	req.OSVersion = "10.0.26200"
	resp, second := a.enroll(req)
	if resp.StatusCode != 201 {
		t.Fatalf("reinscripción: %d %s", resp.StatusCode, bodyString(resp))
	}
	if second.DeviceID != first.DeviceID {
		t.Fatalf("se creó otro registro: %s vs %s", second.DeviceID, first.DeviceID)
	}
	if second.Credential == first.Credential {
		t.Error("la credencial no rotó")
	}
	d := a.device(second.DeviceID)
	if d.CredentialHash != auth.HashSecret(second.Credential) {
		t.Error("credential_hash no se actualizó")
	}
	if d.RevokedAt != nil {
		t.Error("revoked_at no se limpió")
	}
	if d.GroupID == nil || *d.GroupID != g.ID || d.AssignedUser != "ana" || len(d.Tags) != 1 || d.Tags[0] != "vip" {
		t.Errorf("no conservó grupo, usuario o etiquetas: %+v", d)
	}
	if d.OSVersion != "10.0.26200" || !d.EnrolledAt.Equal(enrolledAt) {
		t.Errorf("identidad o fecha de inscripción: %+v", d)
	}
}

func TestEnroll_ReenrollWithDifferentHostnameFlaggedInAudit(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	a.enroll(sampleEnroll(token, "hw-1"))
	req := sampleEnroll(token, "hw-1")
	req.Hostname = "PC-B"
	_, out := a.enroll(req)
	es := a.audit("device.reenrolled")
	if len(es) != 1 || es[0].TargetID != out.DeviceID {
		t.Fatalf("auditoría de reinscripción: %+v", es)
	}
	var d map[string]any
	_ = json.Unmarshal(es[0].Details, &d)
	if d["hostname_changed"] != true || d["previous_hostname"] != "PC-A" || d["hostname"] != "PC-B" {
		t.Fatalf("detalles: %s", es[0].Details)
	}
	// Con el mismo nombre no se señala.
	a.enroll(req)
	es = a.audit("device.reenrolled")
	_ = json.Unmarshal(es[0].Details, &d)
	if d["hostname_changed"] != false {
		t.Fatalf("misma máquina señalada como cambio: %s", es[0].Details)
	}
}

func TestEnroll_AuditOnFirstEnroll(t *testing.T) {
	a := newAgentAPI(t)
	plain, _ := a.tokenWith(store.TokenParams{Name: "Sede central"})
	_, out := a.enroll(sampleEnroll(plain, "hw-1"))
	es := a.audit("device.enrolled")
	if len(es) != 1 || es[0].TargetID != out.DeviceID || es[0].TargetType != "device" || es[0].IP != "203.0.113.10" {
		t.Fatalf("auditoría: %+v", es)
	}
	if !strings.Contains(string(es[0].Details), "Sede central") || strings.Contains(string(es[0].Details), plain) {
		t.Fatalf("detalles: %s", es[0].Details)
	}
	if len(a.audit("device.reenrolled")) != 0 {
		t.Error("la primera inscripción se registró como reinscripción")
	}
}

func TestEnroll_ConcurrentRespectsMaxUses(t *testing.T) {
	a := newAgentAPI(t)
	five := 5
	token := a.token(&five, nil)
	var wg sync.WaitGroup
	codes := make([]int, 20)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := a.enrollFrom(fmt.Sprintf("198.51.100.%d", i+1), sampleEnroll(token, fmt.Sprintf("hw-%d", i)))
			codes[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	ok, forbidden := 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			ok++
		case 403:
			forbidden++
		}
	}
	if ok != 5 || forbidden != 15 {
		t.Fatalf("201=%d 403=%d (%v)", ok, forbidden, codes)
	}
	tokens, _ := a.store.ListTokens(ctx)
	if tokens[0].Uses != 5 {
		t.Fatalf("uses = %d", tokens[0].Uses)
	}
	if n, _ := a.store.CountDevices(ctx); n != 5 {
		t.Fatalf("dispositivos = %d", n)
	}
}

func TestEnroll_ConcurrentSameHardwareIDSingleRecord(t *testing.T) {
	a := newAgentAPI(t)
	token := a.token(nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if resp, _ := a.enrollFrom(fmt.Sprintf("198.51.100.%d", i+1), sampleEnroll(token, "hw-mismo")); resp.StatusCode != 201 {
				t.Errorf("estado %d: %s", resp.StatusCode, bodyString(resp))
			}
		}(i)
	}
	wg.Wait()
	if n, _ := a.store.CountDevices(ctx); n != 1 {
		t.Fatalf("dispositivos = %d", n)
	}
}

func TestEnroll_NulBytesAndLongStringsSanitized(t *testing.T) {
	a := newAgentAPI(t)
	req := sampleEnroll(a.token(nil, nil), "hw-1")
	req.Hostname = "PC\x00-A"
	req.Model = strings.Repeat("ñ", 600)
	resp, out := a.enroll(req)
	if resp.StatusCode != 201 {
		t.Fatalf("estado %d: %s", resp.StatusCode, bodyString(resp))
	}
	d := a.device(out.DeviceID)
	if d.Hostname != "PC-A" || len([]rune(d.Model)) != 512 {
		t.Fatalf("hostname=%q len(model)=%d", d.Hostname, len([]rune(d.Model)))
	}
}
