// Package update actualiza el agente: descarga la versión nueva, comprueba su
// suma, la prueba (un check-in real) antes de sustituir la actual y termina
// para que el gestor de servicios arranque la nueva.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mdm/internal/protocol"
)

const (
	// ExitCode es el código con el que termina el agente tras sustituirse.
	ExitCode      = 75
	retryAfter    = 24 * time.Hour
	selfTestLimit = 60 * time.Second
)

type Deps struct {
	Version  string
	Download func(ctx context.Context, url, dst string) error
	// SelfTest ejecuta el binario nuevo en modo prueba y devuelve error si falla.
	SelfTest func(ctx context.Context, binPath string) error
	Swap     func(newPath, currentPath string) error
	Exit     func(code int)
	BinPath  string
	// Dir es donde se descarga: el directorio protegido de la configuración.
	Dir string
	Now func() time.Time
	Log *slog.Logger
}

type Updater struct {
	d  Deps
	wg sync.WaitGroup

	mu       sync.Mutex
	busy     bool
	rejected map[string]time.Time
	lastErr  string
}

func New(d Deps) *Updater {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Updater{d: d, rejected: map[string]time.Time{}}
}

// OldPath es donde queda el binario anterior tras sustituirlo.
func OldPath(bin string) string {
	if strings.HasSuffix(strings.ToLower(bin), ".exe") {
		return bin[:len(bin)-4] + ".old.exe"
	}
	return bin + ".old"
}

// Offer recibe la actualización del check-in. No bloquea: la descarga y la
// prueba van en segundo plano, y solo una a la vez.
func (u *Updater) Offer(up protocol.AgentUpdate) {
	if up.Version == "" || up.Version == u.d.Version {
		return
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return
	}
	if at, ok := u.rejected[up.Version]; ok && u.d.Now().Sub(at) < retryAfter {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.mu.Unlock()
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		err := u.install(up)
		u.mu.Lock()
		u.busy = false
		if err != nil {
			u.rejected[up.Version] = u.d.Now()
			u.lastErr = fmt.Sprintf("actualización a %s: %v", up.Version, err)
		}
		u.mu.Unlock()
		if err != nil {
			u.d.Log.Warn("actualización descartada; se reintentará en 24 h", "version", up.Version, "err", err)
		}
	}()
}

// Wait espera a que termine el intento en curso (para pruebas y cierre ordenado).
func (u *Updater) Wait() { u.wg.Wait() }

// LastError devuelve el motivo del último intento fallido.
func (u *Updater) LastError() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastErr
}

func (u *Updater) install(up protocol.AgentUpdate) error {
	name := "mdm-agent.new"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tmp := filepath.Join(u.d.Dir, name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cleanup := func() { _ = os.Remove(tmp) }
	if err := u.d.Download(ctx, up.URL, tmp); err != nil {
		cleanup()
		return fmt.Errorf("descarga: %w", err)
	}
	got, err := fileSHA256(tmp)
	if err != nil || !strings.EqualFold(got, up.SHA256) {
		cleanup()
		return fmt.Errorf("la suma de comprobación no coincide")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		cleanup()
		return err
	}
	tctx, tcancel := context.WithTimeout(ctx, selfTestLimit)
	defer tcancel()
	if err := u.d.SelfTest(tctx, tmp); err != nil {
		cleanup()
		return fmt.Errorf("la prueba previa falló: %w", err)
	}
	if err := u.d.Swap(tmp, u.d.BinPath); err != nil {
		cleanup()
		return fmt.Errorf("sustitución: %w", err)
	}
	u.d.Log.Info("agente actualizado; reiniciando", "version", up.Version)
	u.d.Exit(ExitCode)
	return nil
}

// CleanupOld borra el binario anterior. Se llama tras el primer check-in
// correcto del binario nuevo.
func (u *Updater) CleanupOld() {
	_ = os.Remove(OldPath(u.d.BinPath))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Swap sustituye el binario en uso: lo renombra a .old (en Windows se puede
// renombrar un ejecutable en uso, no sobrescribirlo) y coloca el nuevo. Si
// lo segundo falla, deshace lo primero.
func Swap(newPath, current string) error {
	old := OldPath(current)
	_ = os.Remove(old)
	if err := os.Rename(current, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, current); err != nil {
		_ = os.Rename(old, current)
		return err
	}
	return nil
}
