package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mdm/internal/protocol"
)

const newBinary = "binario nuevo"

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type harness struct {
	mu        sync.Mutex
	downloads int
	tests     int
	swaps     int
	exits     []int
	testErr   error
	content   string
	now       time.Time
	dir       string
	u         *Updater
}

func newHarness(t *testing.T) *harness {
	h := &harness{content: newBinary, now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), dir: t.TempDir()}
	bin := filepath.Join(h.dir, "mdm-agent")
	_ = os.WriteFile(bin, []byte("binario viejo"), 0o755)
	h.u = New(Deps{
		Version: "1.0.0",
		Download: func(_ context.Context, url, dst string) error {
			h.mu.Lock()
			h.downloads++
			c := h.content
			h.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			return os.WriteFile(dst, []byte(c), 0o600)
		},
		SelfTest: func(_ context.Context, path string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.tests++
			return h.testErr
		},
		Swap: func(newPath, current string) error {
			h.mu.Lock()
			h.swaps++
			h.mu.Unlock()
			return Swap(newPath, current)
		},
		Exit:    func(code int) { h.mu.Lock(); h.exits = append(h.exits, code); h.mu.Unlock() },
		BinPath: bin,
		Dir:     h.dir,
		Now:     func() time.Time { return h.now },
	})
	return h
}

func offer(v string) protocol.AgentUpdate {
	return protocol.AgentUpdate{Version: v, URL: "/downloads/mdm-agent-linux-amd64", SHA256: sum(newBinary)}
}

func TestUpdater_HappyPath(t *testing.T) {
	h := newHarness(t)
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.downloads != 1 || h.tests != 1 || h.swaps != 1 || len(h.exits) != 1 || h.exits[0] != 75 {
		t.Fatalf("descargas=%d pruebas=%d cambios=%d salidas=%v", h.downloads, h.tests, h.swaps, h.exits)
	}
	b, _ := os.ReadFile(filepath.Join(h.dir, "mdm-agent"))
	old, _ := os.ReadFile(filepath.Join(h.dir, "mdm-agent.old"))
	if string(b) != newBinary || string(old) != "binario viejo" {
		t.Fatalf("binario: %q, anterior: %q", b, old)
	}
}

func TestUpdater_BadChecksum(t *testing.T) {
	h := newHarness(t)
	h.content = "manipulado"
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.tests != 0 || h.swaps != 0 || len(h.exits) != 0 {
		t.Fatal("una suma incorrecta no debe probarse ni instalarse")
	}
	entries, _ := os.ReadDir(h.dir)
	if len(entries) != 1 {
		t.Fatalf("la descarga debe borrarse: %v", entries)
	}
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.downloads != 1 {
		t.Fatal("la misma versión fallida no se reintenta antes de 24 h")
	}
	h.now = h.now.Add(24*time.Hour + time.Minute)
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.downloads != 2 {
		t.Fatal("pasadas 24 h se vuelve a intentar")
	}
}

func TestUpdater_SelfTestFails(t *testing.T) {
	h := newHarness(t)
	h.testErr = errors.New("no arranca")
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.swaps != 0 || len(h.exits) != 0 {
		t.Fatal("si la prueba previa falla no se sustituye")
	}
	b, _ := os.ReadFile(filepath.Join(h.dir, "mdm-agent"))
	if string(b) != "binario viejo" {
		t.Fatal("el binario actual no debe tocarse")
	}
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.downloads != 1 {
		t.Fatal("ignorada durante 24 h")
	}
	if h.u.LastError() == "" {
		t.Fatal("el motivo debe quedar disponible para el estado del agente")
	}
}

func TestUpdater_SameVersionIgnored(t *testing.T) {
	h := newHarness(t)
	h.u.Offer(offer("1.0.0"))
	h.u.Wait()
	if h.downloads != 0 {
		t.Fatal("la versión actual no se descarga")
	}
}

func TestUpdater_OneAtATime(t *testing.T) {
	h := newHarness(t)
	h.u.Offer(offer("1.1.0"))
	h.u.Offer(offer("1.1.0"))
	h.u.Wait()
	if h.downloads != 1 {
		t.Fatalf("descargas: %d", h.downloads)
	}
}

func TestCleanupOld(t *testing.T) {
	h := newHarness(t)
	old := filepath.Join(h.dir, "mdm-agent.old")
	_ = os.WriteFile(old, []byte("x"), 0o755)
	h.u.CleanupOld()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("el binario anterior debe borrarse")
	}
	h.u.CleanupOld() // sin fichero no falla
}

func TestOldPath(t *testing.T) {
	if OldPath(`C:\Program Files\MDMAgent\mdm-agent.exe`) != `C:\Program Files\MDMAgent\mdm-agent.old.exe` {
		t.Fatal("Windows")
	}
	if OldPath("/usr/local/bin/mdm-agent") != "/usr/local/bin/mdm-agent.old" {
		t.Fatal("Linux")
	}
}
