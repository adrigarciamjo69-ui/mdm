package tempadmin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

type fakeRun struct {
	calls []call
	reply func(name string, args []string) (string, error)
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name, args})
	out, err := f.reply(name, args)
	return []byte(out), err
}

const netOutput = "Nombre de alias     Administradores\r\nComentario          Los administradores tienen acceso completo\r\n\r\nMiembros\r\n\r\n" +
	"-------------------------------------------------------------------------------\r\nAdministrador\r\nEMPRESA\\Admins. del dominio\r\nEMPRESA\\jefe\r\n" +
	"Se ha completado el comando correctamente.\r\n\r\n"

func TestParseNetLocalgroup(t *testing.T) {
	got := parseNetLocalgroup(netOutput)
	if strings.Join(got, "|") != `Administrador|EMPRESA\Admins. del dominio|EMPRESA\jefe` {
		t.Fatalf("miembros: %q", got)
	}
	if len(parseNetLocalgroup("Miembros\r\n\r\n-----\r\nSe ha completado el comando correctamente.\r\n")) != 0 {
		t.Fatal("grupo vacío")
	}
}

func TestMemberMatches(t *testing.T) {
	members := []string{"Administrador", `EMPRESA\jefe`, `AzureAD\AnaLopez`}
	cases := map[string]bool{
		`empresa\JEFE`: true, "jefe": true, "administrador": true, `EMPRESA\ana`: false,
		`OTRO\jefe`: false, `AzureAD\analopez`: true, `EMPRESA\Administrador`: false,
	}
	for u, want := range cases {
		if got := memberMatches(members, u); got != want {
			t.Errorf("%s: %v", u, got)
		}
	}
}

func TestWindowsOS(t *testing.T) {
	f := &fakeRun{reply: func(name string, args []string) (string, error) {
		switch {
		case name == "powershell.exe" && strings.Contains(strings.Join(args, " "), "S-1-5-32-544"):
			return "BUILTIN\\Administradores\r\n", nil
		case name == "powershell.exe":
			return "EMPRESA\\ana\r\n", nil
		case name == "net.exe" && len(args) == 2:
			return netOutput, nil
		}
		return "Se ha completado el comando correctamente.", nil
	}}
	w := &Windows{Run: f.run}
	if u, err := w.CurrentUser(ctx); err != nil || u != `EMPRESA\ana` {
		t.Fatalf("usuario: %q %v", u, err)
	}
	if ok, _ := w.IsAdmin(ctx, `EMPRESA\jefe`); !ok {
		t.Fatal("jefe es administrador")
	}
	if ok, _ := w.IsAdmin(ctx, `EMPRESA\ana`); ok {
		t.Fatal("ana no")
	}
	f.calls = nil
	if err := w.Add(ctx, `EMPRESA\ana`); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if last.name != "net.exe" || strings.Join(last.args, "|") != `localgroup|Administradores|EMPRESA\ana|/add` {
		t.Fatalf("añadir: %+v", last)
	}
	// Quitar a quien no está no ejecuta nada y no falla.
	f.calls = nil
	if err := w.Remove(ctx, `EMPRESA\ana`); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if len(c.args) > 0 && c.args[len(c.args)-1] == "/delete" {
			t.Fatal("no debe intentar quitar a quien no está")
		}
	}
	if err := w.Remove(ctx, `EMPRESA\jefe`); err != nil {
		t.Fatal(err)
	}
	last = f.calls[len(f.calls)-1]
	if strings.Join(last.args, "|") != `localgroup|Administradores|EMPRESA\jefe|/delete` {
		t.Fatalf("quitar: %+v", last)
	}
}

func TestWindowsOS_NoSession(t *testing.T) {
	w := &Windows{Run: (&fakeRun{reply: func(string, []string) (string, error) { return "\r\n", nil }}).run}
	if _, err := w.CurrentUser(ctx); err == nil {
		t.Fatal("sin sesión")
	}
}

func TestLinuxOS(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "etc/group"), []byte("root:x:0:\nsudo:x:27:jefe\nusers:x:100:\n"), 0o644)
	f := &fakeRun{reply: func(name string, args []string) (string, error) {
		switch name {
		case "who":
			return "root     pts/0  2026-11-25 09:00\nana      tty2   2026-11-25 09:01 (:0)\n", nil
		case "id":
			switch args[len(args)-1] {
			case "jefe":
				return "jefe sudo users\n", nil
			case "ana":
				return "ana users\n", nil
			}
			return "id: ‘pepe’: no such user", errors.New("exit status 1")
		}
		return "", nil
	}}
	l := &Linux{Run: f.run, Root: root}
	if u, err := l.CurrentUser(ctx); err != nil || u != "ana" {
		t.Fatalf("usuario: %q %v", u, err)
	}
	if ok, _ := l.IsAdmin(ctx, "jefe"); !ok {
		t.Fatal("jefe está en sudo")
	}
	if ok, err := l.IsAdmin(ctx, "ana"); ok || err != nil {
		t.Fatalf("ana: %v %v", ok, err)
	}
	if _, err := l.IsAdmin(ctx, "pepe"); err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Fatalf("usuario inexistente: %v", err)
	}
	f.calls = nil
	_ = l.Add(ctx, "ana")
	if c := f.calls[len(f.calls)-1]; c.name != "usermod" || strings.Join(c.args, " ") != "-aG sudo ana" {
		t.Fatalf("añadir: %+v", c)
	}
	_ = l.Remove(ctx, "jefe")
	if c := f.calls[len(f.calls)-1]; c.name != "gpasswd" || strings.Join(c.args, " ") != "-d jefe sudo" {
		t.Fatalf("quitar: %+v", c)
	}
	// Sin grupo sudo, se usa wheel.
	_ = os.WriteFile(filepath.Join(root, "etc/group"), []byte("root:x:0:\nwheel:x:10:\n"), 0o644)
	f.calls = nil
	_ = l.Add(ctx, "ana")
	if c := f.calls[len(f.calls)-1]; strings.Join(c.args, " ") != "-aG wheel ana" {
		t.Fatalf("wheel: %+v", c)
	}
}

func TestWindowsOS_CurrentUserFallsBackToSessions(t *testing.T) {
	// Win32_ComputerSystem.UserName vacío (Escritorio remoto, cambio rápido de usuario).
	w := &Windows{Run: (&fakeRun{reply: func(string, []string) (string, error) { return "\r\n", nil }}).run,
		Session: func() string { return `ELECTRANS\fresadora` }}
	if u, err := w.CurrentUser(ctx); err != nil || u != `ELECTRANS\fresadora` {
		t.Fatalf("usuario de la sesión: %q %v", u, err)
	}
}
