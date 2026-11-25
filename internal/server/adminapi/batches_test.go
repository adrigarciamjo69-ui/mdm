package adminapi

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

type batchResp struct {
	ID                string         `json:"id"`
	Type              string         `json:"type"`
	DeviceCount       int            `json:"device_count"`
	TargetDescription string         `json:"target_description"`
	CreatedByEmail    string         `json:"created_by_email"`
	Counts            map[string]int `json:"counts"`
	Commands          []struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
	} `json:"commands"`
}

type createBatchResp struct {
	Batch   batchResp `json:"batch"`
	Skipped []struct {
		Hostname string `json:"hostname"`
		Reason   string `json:"reason"`
	} `json:"skipped"`
}

func TestBatches_RolesByType(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{})
	ids := []string{id.String()}
	op, ro, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleReadonly), a.loginAs(auth.RoleAdmin)
	for _, typ := range []string{"reboot", "shutdown", "refresh_inventory", "evaluate_policies"} {
		wantStatus(t, op.do("POST", "/batches", map[string]any{"type": typ, "device_ids": ids}), 201)
		wantError(t, ro.do("POST", "/batches", map[string]any{"type": typ, "device_ids": ids}), 403, "forbidden")
	}
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "retire", "device_ids": ids}), 403, "forbidden")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "run_script", "device_ids": ids}), 403, "forbidden")
	wantStatus(t, admin.do("POST", "/batches", map[string]any{"type": "retire", "device_ids": ids}), 201)
	wantError(t, admin.do("POST", "/batches", map[string]any{"type": "formatear", "device_ids": ids}), 400, "invalid_request")
}

func TestBatches_PowerParamsValidated(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{}).String()
	op := a.loginAs(auth.RoleOperator)
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{id}, "params": map[string]any{"delay_s": 3601}}), 400, "invalid_request")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{id}, "params": map[string]any{"message": strings.Repeat("x", 201)}}), 400, "invalid_request")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "refresh_inventory", "device_ids": []string{id}, "params": map[string]any{"x": 1}}), 400, "invalid_request")
	resp := op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{id}, "params": map[string]any{"delay_s": 300, "message": "Aviso"}})
	wantStatus(t, resp, 201)
}

func TestBatches_GroupTargetSkipsRevokedAndLimit(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	a.seedDevice(seed{Hostname: "v1", Group: &g.ID})
	a.seedDevice(seed{Hostname: "v2", Group: &g.ID})
	a.seedDevice(seed{Hostname: "v3", Group: &g.ID, Revoked: true})
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/batches", map[string]any{"type": "refresh_inventory", "group_id": g.ID})
	wantStatus(t, resp, 201)
	out := decode[createBatchResp](t, resp)
	if out.Batch.DeviceCount != 2 || len(out.Skipped) != 1 || out.Skipped[0].Hostname != "v3" || !strings.Contains(out.Batch.TargetDescription, "Ventas") {
		t.Fatalf("lote por grupo: %+v", out)
	}
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot"}), 400, "invalid_request")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{}}), 400, "invalid_request")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{uuid.NewString()}}), 400, "invalid_request")
	many := make([]string, 1001)
	for i := range many {
		many[i] = uuid.NewString()
	}
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": many}), 400, "invalid_request")
}

func TestBatches_ProgressAndDetail(t *testing.T) {
	a := newTestAPI(t)
	d1 := a.seedDevice(seed{Hostname: "b"})
	d2 := a.seedDevice(seed{Hostname: "a"})
	op := a.loginAs(auth.RoleOperator)
	out := decode[createBatchResp](t, op.do("POST", "/batches", map[string]any{"type": "refresh_inventory", "device_ids": []string{d1.String(), d2.String()}}))
	cmds, _ := a.store.NextCommands(ctx, d1, []string{protocol.CapCommands}, a.clock.Now(), 10)
	_, _ = a.store.FinishCommand(ctx, d1, uuid.MustParse(cmds[0].ID), protocol.CommandResult{Status: "succeeded"}, a.clock.Now())
	b := decode[batchResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/batches/"+out.Batch.ID, nil))
	if b.Counts["succeeded"] != 1 || b.Counts["pending"] != 1 || len(b.Commands) != 2 || b.Commands[0].Hostname != "a" || b.CreatedByEmail != op.admin.Email {
		t.Fatalf("detalle: %+v", b)
	}
	list := decode[pageResp[batchResp]](t, op.do("GET", "/batches", nil))
	if list.Total != 1 || list.Items[0].Counts["succeeded"] != 1 {
		t.Fatalf("listado: %+v", list)
	}
	type cmdResp struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	dc := decode[[]cmdResp](t, op.do("GET", "/devices/"+d1.String()+"/commands", nil))
	if len(dc) != 1 || dc[0].Status != "succeeded" {
		t.Fatalf("órdenes del equipo: %+v", dc)
	}
}

func TestCommandDetailAndCancel(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{})
	op := a.loginAs(auth.RoleOperator)
	out := decode[createBatchResp](t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{d.String()}}))
	cid := out.Batch.Commands[0].ID
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/commands/"+cid+"/cancel", nil), 403, "forbidden")
	wantStatus(t, op.do("POST", "/commands/"+cid+"/cancel", nil), 204)
	type cmdResp struct {
		Status string `json:"status"`
		Stdout string `json:"stdout"`
	}
	if c := decode[cmdResp](t, op.do("GET", "/commands/"+cid, nil)); c.Status != "cancelled" {
		t.Fatalf("tras cancelar: %+v", c)
	}
	out2 := decode[createBatchResp](t, op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{d.String()}}))
	_ = a.store.StartCommand(ctx, d, uuid.MustParse(out2.Batch.Commands[0].ID), a.clock.Now())
	wantError(t, op.do("POST", "/commands/"+out2.Batch.Commands[0].ID+"/cancel", nil), 409, "conflict")
	wantError(t, op.do("GET", "/commands/"+uuid.NewString(), nil), 404, "not_found")
	if e := a.findAudit("command.cancelled"); e == nil || e.TargetID != cid {
		t.Fatalf("auditoría: %+v", e)
	}
}

func TestBatches_Audit(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{})
	op := a.loginAs(auth.RoleOperator)
	op.do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": []string{d.String()}})
	e := a.findAudit("batch.created")
	if e == nil || !strings.Contains(string(e.Details), `"type": "reboot"`) || !strings.Contains(string(e.Details), `"devices": 1`) {
		t.Fatalf("auditoría: %+v", e)
	}
	_ = time.Now
	_ = fmt.Sprint
}

func TestBatches_TempAdmin(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{}).String()
	ids := []string{id}
	op, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleAdmin)
	good := map[string]any{"user": `EMPRESA\ana.lopez`, "minutes": 60, "reason": "Instalar el driver de la impresora"}
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "temp_admin", "device_ids": ids, "params": good}), 403, "forbidden")
	wantError(t, op.do("POST", "/batches", map[string]any{"type": "temp_admin_revoke", "device_ids": ids}), 403, "forbidden")
	for _, p := range []map[string]any{
		{"user": "ana", "minutes": 10, "reason": "Instalar"},
		{"user": "ana", "minutes": 481, "reason": "Instalar"},
		{"user": "ana", "minutes": 60, "reason": ""},
		{"user": "ana", "minutes": 60, "reason": strings.Repeat("x", 501)},
		{"user": "ana\" & net user x /add", "minutes": 60, "reason": "Instalar"},
		{"user": "ana", "minutes": 60, "reason": "Instalar", "extra": true},
	} {
		wantError(t, admin.do("POST", "/batches", map[string]any{"type": "temp_admin", "device_ids": ids, "params": p}), 400, "invalid_request")
	}
	wantStatus(t, admin.do("POST", "/batches", map[string]any{"type": "temp_admin", "device_ids": ids, "params": good}), 201)
	// Sin usuario: el que tenga la sesión iniciada.
	wantStatus(t, admin.do("POST", "/batches", map[string]any{"type": "temp_admin", "device_ids": ids,
		"params": map[string]any{"minutes": 15, "reason": "Soporte"}}), 201)
	wantStatus(t, admin.do("POST", "/batches", map[string]any{"type": "temp_admin_revoke", "device_ids": ids}), 201)
	wantStatus(t, admin.do("POST", "/batches", map[string]any{"type": "temp_admin_revoke", "device_ids": ids,
		"params": map[string]any{"user": "ana"}}), 201)

	e := a.findAudit("batch.created")
	if e == nil {
		t.Fatal("sin auditoría")
	}
	var found bool
	for _, e := range a.auditEntries() {
		d := string(e.Details)
		if e.Action == "batch.created" && strings.Contains(d, "Instalar el driver de la impresora") && strings.Contains(d, `"minutes": 60`) {
			found = true
		}
	}
	if !found {
		t.Fatal("la auditoría debe guardar el usuario, los minutos y el motivo")
	}
}

func TestTempAdmin_NeedsCapability(t *testing.T) {
	if protocol.RequiredCapability(protocol.CmdTempAdmin) != protocol.CapTempAdmin ||
		protocol.RequiredCapability(protocol.CmdTempAdminRevoke) != protocol.CapTempAdmin {
		t.Fatal("capacidad del administrador temporal")
	}
}

func TestBatches_TempAdminExpiresSoon(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{}).String()
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("POST", "/batches", map[string]any{"type": "temp_admin", "device_ids": []string{id},
		"params": map[string]any{"minutes": 60, "reason": "Soporte"}}), 201)
	var created, expires time.Time
	_ = a.store.Pool().QueryRow(ctx, `SELECT created_at, expires_at FROM commands`).Scan(&created, &expires)
	if expires.Sub(created) != 4*time.Hour {
		t.Fatalf("un permiso pedido ahora no debe llegar días después: caduca en %s", expires.Sub(created))
	}
}
