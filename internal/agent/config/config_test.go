package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func sample() Config {
	return Config{ServerURL: "https://mdm.ejemplo.es", DeviceID: "0b0e7a2e-1111-4222-8333-444455556666",
		Credential: "secreto", CheckinIntervalS: 60, InventoryIntervalS: 21600}
}

func TestConfig_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agente", "agent.json")
	want := sample()
	want.Revoked = true
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got.Bearer() != "0b0e7a2e-1111-4222-8333-444455556666.secreto" {
		t.Errorf("Bearer: %s", got.Bearer())
	}
}

func TestConfig_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("en Windows los permisos se fijan con ACL")
	}
	dir := filepath.Join(t.TempDir(), "agente")
	path := filepath.Join(dir, "agent.json")
	if err := Save(path, sample()); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("fichero %o, directorio %o", fi.Mode().Perm(), di.Mode().Perm())
	}
	// Guardar de nuevo sobre un fichero con permisos más abiertos los vuelve a cerrar.
	_ = os.Chmod(path, 0o644)
	_ = Save(path, sample())
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("tras regrabar: %o", fi.Mode().Perm())
	}
}

func TestConfig_LoadMissing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "no-existe.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestConfig_LoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	_ = os.WriteFile(path, []byte("{a medias"), 0o600)
	if _, err := Load(path); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("un fichero corrupto debe dar un error propio: %v", err)
	}
}

func TestConfig_SaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	for i := 0; i < 3; i++ {
		if err := Save(path, sample()); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "agent.json" {
		t.Fatalf("quedan ficheros temporales: %v", entries)
	}
}

func TestState_RoundTripAndMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if st, err := LoadState(path); err != nil || st.LastSuccess != nil || st.LastError != "" {
		t.Fatalf("estado inexistente debe ser vacío sin error: %+v %v", st, err)
	}
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	want := State{LastSuccess: &now, LastError: "sin red", LastErrorAt: &now}
	if err := SaveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil || got.LastError != "sin red" || !got.LastSuccess.Equal(now) || !got.LastErrorAt.Equal(now) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agente")
	_ = Save(filepath.Join(dir, "agent.json"), sample())
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el directorio sigue ahí: %v", err)
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("borrar dos veces no debe fallar: %v", err)
	}
}

func TestPaths(t *testing.T) {
	if filepath.Dir(Path()) != Dir() || filepath.Dir(StatePath()) != Dir() || filepath.Base(Path()) != "agent.json" {
		t.Fatalf("rutas: %s %s %s", Dir(), Path(), StatePath())
	}
	if runtime.GOOS == "linux" && Dir() != "/etc/mdm-agent" {
		t.Fatalf("directorio en Linux: %s", Dir())
	}
}

func TestSave_RestrictsDirectoryOncePerProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agente")
	calls := 0
	orig := restrict
	restrict = func(d string) error { calls++; return orig(d) }
	defer func() { restrict = orig }()
	for i := 0; i < 5; i++ {
		if err := SaveState(filepath.Join(dir, "state.json"), State{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("se protegió el directorio %d veces; debe hacerse una vez", calls)
	}
}
