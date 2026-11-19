package agentapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
)

func (a *agentTest) queue(typ string, payload any, ids ...uuid.UUID) store.Batch {
	a.t.Helper()
	b, _ := json.Marshal(payload)
	bt, err := a.store.CreateBatch(ctx, store.NewBatch{Type: typ, Params: b, At: a.clock.Now()}, ids)
	if err != nil {
		a.t.Fatal(err)
	}
	return bt
}

var caps = []string{protocol.CapCommands, protocol.CapScripts}

func TestCheckin_DeliversCommands(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	b := a.queue(protocol.CmdReboot, protocol.PowerPayload{DelayS: 300, Message: "Reinicio programado"}, c.id)
	_, out := c.checkin(protocol.CheckinRequest{Capabilities: caps})
	if len(out.Commands) != 1 || out.Commands[0].ID != b.CommandIDs[0].String() || out.Commands[0].Type != "reboot" {
		t.Fatalf("órdenes: %+v", out.Commands)
	}
	var p protocol.PowerPayload
	if err := json.Unmarshal(out.Commands[0].Payload, &p); err != nil || p.DelayS != 300 || p.Message != "Reinicio programado" {
		t.Fatalf("carga: %s", out.Commands[0].Payload)
	}
	// Un agente sin capacidades (fase 1) no recibe nada.
	other := a.enrolled()
	a.queue(protocol.CmdReboot, protocol.PowerPayload{}, other.id)
	if _, out := other.checkin(protocol.CheckinRequest{}); len(out.Commands) != 0 {
		t.Fatalf("un agente de la fase 1 no debe recibir órdenes: %+v", out.Commands)
	}
}

func TestCommandLifecycleOverAPI(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	b := a.queue(protocol.CmdRunScript, protocol.ScriptPayload{Interpreter: "sh", Content: "echo hola", TimeoutS: 60}, c.id)
	id := b.CommandIDs[0].String()
	if resp := a.post("/commands/"+id+"/start", "", c.authz, struct{}{}, false); resp.StatusCode != 204 {
		t.Fatalf("start: %d %s", resp.StatusCode, bodyString(resp))
	}
	code := 0
	res := protocol.CommandResult{Status: "succeeded", ExitCode: &code, Stdout: "hola\n", StartedAt: a.clock.Now(), FinishedAt: a.clock.Now()}
	if resp := a.post("/commands/"+id+"/result", "", c.authz, res, false); resp.StatusCode != 204 {
		t.Fatalf("result: %d %s", resp.StatusCode, bodyString(resp))
	}
	got, _ := a.store.GetCommand(ctx, b.CommandIDs[0])
	if got.Status != "succeeded" || got.Stdout != "hola\n" {
		t.Fatalf("orden: %+v", got)
	}
	// Repetir el resultado no falla.
	if resp := a.post("/commands/"+id+"/result", "", c.authz, res, false); resp.StatusCode != 204 {
		t.Fatalf("resultado repetido: %d", resp.StatusCode)
	}
}

func TestCommandEndpoints_Errors(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	other := a.enrolled()
	b := a.queue(protocol.CmdReboot, protocol.PowerPayload{}, c.id)
	id := b.CommandIDs[0].String()
	wantError(t, a.post("/commands/"+id+"/start", "", other.authz, struct{}{}, false), 404, "not_found")
	wantError(t, a.post("/commands/"+id+"/result", "", other.authz, protocol.CommandResult{Status: "succeeded"}, false), 404, "not_found")
	wantError(t, a.post("/commands/no-uuid/start", "", c.authz, struct{}{}, false), 404, "not_found")
	wantError(t, a.post("/commands/"+id+"/result", "", c.authz, protocol.CommandResult{Status: "running"}, false), 400, "invalid_request")
	big := protocol.CommandResult{Status: "failed", Stdout: strings.Repeat("x", 400*1024)}
	wantError(t, a.post("/commands/"+id+"/result", "", c.authz, big, false), 413, "too_large")
}

func TestRetireOverAPI_RevokesAndAudits(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	b := a.queue(protocol.CmdRetire, nil, c.id)
	resp := a.post("/commands/"+b.CommandIDs[0].String()+"/result", "", c.authz, protocol.CommandResult{Status: "succeeded", FinishedAt: time.Now()}, false)
	if resp.StatusCode != 204 {
		t.Fatalf("%d", resp.StatusCode)
	}
	if d := a.device(c.id.String()); d.RevokedAt == nil {
		t.Fatal("no quedó revocado")
	}
	if len(a.audit("device.retired")) != 1 {
		t.Fatal("falta la auditoría device.retired")
	}
	if resp, _ := c.checkin(protocol.CheckinRequest{}); resp.StatusCode != 401 {
		t.Fatalf("un equipo retirado ya no puede hacer check-in: %d", resp.StatusCode)
	}
}
