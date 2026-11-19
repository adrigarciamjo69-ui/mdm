// Package settings implementa en el agente los ajustes del catálogo de
// políticas: cada uno sabe comprobarse y, si se puede, aplicarse.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

// ErrNotApplicable indica que el ajuste no tiene sentido en este equipo
// (por ejemplo, sin TPM o sin servidor SSH).
var ErrNotApplicable = errors.New("no aplicable en este equipo")

// Setting es un ajuste concreto con sus parámetros.
type Setting interface {
	// Check informa de si se cumple, con un detalle legible.
	Check(ctx context.Context) (compliant bool, detail string, err error)
	// Apply intenta que se cumpla.
	Apply(ctx context.Context) error
}

// RecoveryReporter lo implementan los ajustes que custodian una clave de
// recuperación (BitLocker): la devuelven tras comprobarse si hay que enviarla.
type RecoveryReporter interface {
	Recovery() *protocol.RecoveryKey
}

// Runner ejecuta una orden del sistema y devuelve su salida. Si termina con
// error pero produjo salida, devuelve ambas.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Deps struct {
	Run  Runner
	Exec func(ctx context.Context, s exec.Spec) exec.Result
	// Has informa de si una orden está instalada.
	Has func(name string) bool
	// Root es la raíz del sistema de ficheros («/» en un equipo real).
	Root      string
	ScriptDir string
	// Escrowed son los protectores de BitLocker ya custodiados.
	Escrowed []string
}

func (d Deps) path(rel string) string { return filepath.Join(d.Root, rel) }

func (d Deps) readFile(rel string) string {
	b, _ := os.ReadFile(d.path(rel))
	return string(b)
}

func (d Deps) exists(rel string) bool {
	_, err := os.Stat(d.path(rel))
	return err == nil
}

func (d Deps) writeFile(rel, content string, perm os.FileMode) error {
	p := d.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".mdm-tmp"
	if err := os.WriteFile(tmp, []byte(content), perm); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (d Deps) run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := d.Run(ctx, name, args...)
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type builder func(params json.RawMessage, d Deps) (Setting, error)

var builders = map[string]map[string]builder{
	protocol.PlatformLinux:   {},
	protocol.PlatformWindows: {},
}

func register(platform, key string, b builder) { builders[platform][key] = b }

// Build crea el ajuste de la política para la plataforma.
func Build(platform string, s protocol.PolicySetting, d Deps) (Setting, error) {
	b, ok := builders[platform][s.Key]
	if !ok {
		return nil, fmt.Errorf("este agente no conoce el ajuste %q", s.Key)
	}
	params := s.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}
	return b(params, d)
}

// Keys devuelve las claves implementadas para la plataforma.
func Keys(platform string) []string {
	var out []string
	for k := range builders[platform] {
		out = append(out, k)
	}
	return out
}

// lastLine devuelve la última línea no vacía, recortada, como detalle.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if len([]rune(l)) > 200 {
		l = string([]rune(l)[:200])
	}
	return l
}
