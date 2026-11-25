package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var (
	t0      = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	allCaps = []string{protocol.CapCommands, protocol.CapScripts}
)

func enrollOne(t *testing.T, s *store.Store, hw string) uuid.UUID {
	t.Helper()
	adm, err := s.AdminByEmail(ctx, "a@ejemplo.es")
	if err != nil {
		adm, _ = s.CreateAdmin(ctx, "a@ejemplo.es", "x", "admin")
	}
	_, _ = s.CreateToken(ctx, store.TokenParams{Name: "t", TokenHash: "h-" + hw, CreatedBy: adm.ID})
	res, err := s.EnrollDevice(ctx, store.EnrollParams{TokenHash: "h-" + hw, CredentialHash: "c", At: t0,
		Req: protocol.EnrollRequest{HardwareID: hw, Hostname: hw, Platform: "linux"}})
	if err != nil {
		t.Fatal(err)
	}
	return res.DeviceID
}

func batch(t *testing.T, s *store.Store, typ string, payload any, at time.Time, ids ...uuid.UUID) store.Batch {
	t.Helper()
	b, _ := json.Marshal(payload)
	bt, err := s.CreateBatch(ctx, store.NewBatch{Type: typ, Params: b, At: at, TargetDescription: "prueba"}, ids)
	if err != nil {
		t.Fatal(err)
	}
	return bt
}

func TestNextCommands_MarksSentAndRedelivers(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	batch(t, s, protocol.CmdReboot, protocol.PowerPayload{DelayS: 60}, t0, d)
	cmds, err := s.NextCommands(ctx, d, allCaps, t0, 10)
	if err != nil || len(cmds) != 1 || cmds[0].Type != "reboot" || !cmds[0].ExpiresAt.Equal(t0.Add(7*24*time.Hour)) {
		t.Fatalf("primera entrega: %+v %v", cmds, err)
	}
	c, _ := s.GetCommand(ctx, uuid.MustParse(cmds[0].ID))
	if c.Status != protocol.CommandSent || c.SentAt == nil {
		t.Fatalf("estado tras entregar: %+v", c)
	}
	again, _ := s.NextCommands(ctx, d, allCaps, t0.Add(time.Minute), 10)
	if len(again) != 1 || again[0].ID != cmds[0].ID {
		t.Fatal("una orden enviada y no iniciada debe volver a entregarse")
	}
	_ = s.StartCommand(ctx, d, uuid.MustParse(cmds[0].ID), t0.Add(2*time.Minute))
	if after, _ := s.NextCommands(ctx, d, allCaps, t0.Add(3*time.Minute), 10); len(after) != 0 {
		t.Fatal("una orden iniciada no se vuelve a entregar")
	}
}

func TestNextCommands_Limit10OldestFirst(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	for i := 0; i < 12; i++ {
		batch(t, s, protocol.CmdRefreshInventory, nil, t0.Add(time.Duration(i)*time.Second), d)
	}
	cmds, _ := s.NextCommands(ctx, d, allCaps, t0.Add(time.Minute), 10)
	if len(cmds) != 10 {
		t.Fatalf("entregadas: %d", len(cmds))
	}
	first, _ := s.GetCommand(ctx, uuid.MustParse(cmds[0].ID))
	if !first.CreatedAt.Equal(t0) {
		t.Fatalf("no se entregó la más antigua primero: %v", first.CreatedAt)
	}
}

func TestNextCommands_SkipsUnsupported(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	script := batch(t, s, protocol.CmdRunScript, protocol.ScriptPayload{Interpreter: "sh", Content: "true", TimeoutS: 60}, t0, d)
	batch(t, s, protocol.CmdReboot, protocol.PowerPayload{}, t0, d)
	cmds, _ := s.NextCommands(ctx, d, []string{protocol.CapCommands}, t0, 10)
	if len(cmds) != 1 || cmds[0].Type != "reboot" {
		t.Fatalf("entregadas a un agente sin scripts: %+v", cmds)
	}
	rows, _ := s.BatchCommands(ctx, script.ID)
	if rows[0].Status != protocol.CommandPending {
		t.Fatalf("la orden no soportada debe seguir pendiente: %s", rows[0].Status)
	}
	if none, _ := s.NextCommands(ctx, d, nil, t0, 10); len(none) != 0 {
		t.Fatal("un agente de la fase 1 no recibe órdenes")
	}
}

func TestNextCommands_SkipsExpired(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	batch(t, s, protocol.CmdReboot, protocol.PowerPayload{}, t0, d)
	if cmds, _ := s.NextCommands(ctx, d, allCaps, t0.Add(7*24*time.Hour+time.Second), 10); len(cmds) != 0 {
		t.Fatal("se entregó una orden caducada")
	}
}

func TestFinishCommand_SanitizesOutput(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	b := batch(t, s, protocol.CmdRunScript, protocol.ScriptPayload{Interpreter: "sh", Content: "x", TimeoutS: 60}, t0, d)
	id := b.CommandIDs[0]
	code := 0
	out := "\x00inicio\xff\xfe" + strings.Repeat("a", 200*1024) + "FINAL"
	if _, err := s.FinishCommand(ctx, d, id, protocol.CommandResult{Status: "succeeded", ExitCode: &code, Stdout: out, Stderr: "e\x00rr"}, t0); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCommand(ctx, id)
	if !utf8.ValidString(c.Stdout) || strings.ContainsRune(c.Stdout, 0) || len(c.Stdout) > 64*1024+len("[salida recortada]\n") ||
		!strings.HasPrefix(c.Stdout, "[salida recortada]\n") || !strings.HasSuffix(c.Stdout, "FINAL") || c.Stderr != "err" {
		t.Fatalf("salida: %d bytes, prefijo %q", len(c.Stdout), c.Stdout[:30])
	}
	if c.Status != protocol.CommandSucceeded || c.ExitCode == nil || *c.ExitCode != 0 || c.FinishedAt == nil {
		t.Fatalf("resultado: %+v", c)
	}
}

func TestFinishCommand_WrongDeviceAndIdempotent(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	other := enrollOne(t, s, "hw2")
	b := batch(t, s, protocol.CmdRefreshInventory, nil, t0, d)
	id := b.CommandIDs[0]
	if _, err := s.FinishCommand(ctx, other, id, protocol.CommandResult{Status: "succeeded"}, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("otro equipo: %v", err)
	}
	if err := s.StartCommand(ctx, other, id, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("otro equipo al iniciar: %v", err)
	}
	if _, err := s.FinishCommand(ctx, d, id, protocol.CommandResult{Status: "succeeded"}, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishCommand(ctx, d, id, protocol.CommandResult{Status: "failed", Error: "otra vez"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCommand(ctx, id)
	if c.Status != protocol.CommandSucceeded || !c.FinishedAt.Equal(t0) {
		t.Fatalf("un segundo resultado no cambia nada: %+v", c)
	}
	if _, err := s.FinishCommand(ctx, d, id, protocol.CommandResult{Status: "running"}, t0); err == nil {
		t.Fatal("un resultado con estado no final debe rechazarse")
	}
}

func TestExpireCommands(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	pend := batch(t, s, protocol.CmdReboot, protocol.PowerPayload{}, t0, d)
	run := batch(t, s, protocol.CmdRunScript, protocol.ScriptPayload{Interpreter: "sh", Content: "x", TimeoutS: 60}, t0, d)
	_ = s.StartCommand(ctx, d, run.CommandIDs[0], t0)
	n, err := s.ExpireCommands(ctx, t0.Add(11*time.Minute+time.Second))
	if err != nil || n != 1 {
		t.Fatalf("la orden en curso con 60 s + 10 min superados: %d %v", n, err)
	}
	c, _ := s.GetCommand(ctx, run.CommandIDs[0])
	if c.Status != protocol.CommandFailed || c.Error != "Sin respuesta del equipo." {
		t.Fatalf("en curso: %+v", c)
	}
	n, _ = s.ExpireCommands(ctx, t0.Add(8*24*time.Hour))
	c, _ = s.GetCommand(ctx, pend.CommandIDs[0])
	if n != 1 || c.Status != protocol.CommandExpired {
		t.Fatalf("pendiente de hace 8 días: %d %+v", n, c)
	}
}

func TestRetireRevokesOnSuccess(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	b := batch(t, s, protocol.CmdRetire, nil, t0, d)
	c, err := s.FinishCommand(ctx, d, b.CommandIDs[0], protocol.CommandResult{Status: "succeeded"}, t0)
	if err != nil || !c.Changed {
		t.Fatalf("%v %+v", err, c)
	}
	if dev, _ := s.DeviceByID(ctx, d); dev.RevokedAt == nil {
		t.Fatal("el equipo retirado debe quedar revocado")
	}
}

func TestCancelCommand(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw1")
	b := batch(t, s, protocol.CmdReboot, protocol.PowerPayload{}, t0, d)
	if err := s.CancelCommand(ctx, b.CommandIDs[0], t0); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := s.NextCommands(ctx, d, allCaps, t0, 10); len(cmds) != 0 {
		t.Fatal("una orden cancelada no se entrega")
	}
	r := batch(t, s, protocol.CmdReboot, protocol.PowerPayload{}, t0, d)
	_ = s.StartCommand(ctx, d, r.CommandIDs[0], t0)
	if err := s.CancelCommand(ctx, r.CommandIDs[0], t0); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("en curso: %v", err)
	}
	if err := s.CancelCommand(ctx, uuid.New(), t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
}

func TestCreateBatch_Limit(t *testing.T) {
	s := storetest.New(t)
	ids := make([]uuid.UUID, 1001)
	for i := range ids {
		ids[i] = uuid.New()
	}
	if _, err := s.CreateBatch(ctx, store.NewBatch{Type: "reboot", At: t0}, ids); !errors.Is(err, store.ErrTooManyDevices) {
		t.Fatalf("1.001 equipos: %v", err)
	}
}

func TestNextCommands_TempAdminNeedsCapability(t *testing.T) {
	s := storetest.New(t)
	d := enrollOne(t, s, "hw-ta")
	batch(t, s, protocol.CmdTempAdmin, protocol.TempAdminPayload{User: "ana", Minutes: 60, Reason: "Soporte"}, t0, d)
	if cmds, _ := s.NextCommands(ctx, d, allCaps, t0, 10); len(cmds) != 0 {
		t.Fatalf("un agente sin tempadmin.v1 no la recibe: %+v", cmds)
	}
	cmds, _ := s.NextCommands(ctx, d, append(allCaps, protocol.CapTempAdmin), t0, 10)
	if len(cmds) != 1 || cmds[0].Type != protocol.CmdTempAdmin {
		t.Fatalf("con la capacidad sí: %+v", cmds)
	}
}
