package app

import (
	"bufio"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mdm/internal/protocol"
)

// Releases describe los binarios del agente que distribuye el servidor, con
// sus sumas de SHA256SUMS. Todos son de la versión del propio servidor.
type Releases struct {
	version string
	sums    map[string]string // nombre de fichero → sha256
}

// LoadReleases lee SHA256SUMS de dir. Sin el fichero no se ofrecen
// actualizaciones, pero no es un error.
func LoadReleases(dir, serverVersion string) (*Releases, error) {
	r := &Releases{version: serverVersion, sums: map[string]string{}}
	f, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if errors.Is(err, fs.ErrNotExist) {
		slog.Warn("sin SHA256SUMS en la carpeta de descargas: no se ofrecerán actualizaciones del agente", "dir", dir)
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if strings.HasPrefix(name, "mdm-agent-") {
			r.sums[name] = fields[0]
		}
	}
	return r, sc.Err()
}

func binaryName(platform, arch string) string {
	name := "mdm-agent-" + platform + "-" + arch
	if platform == protocol.PlatformWindows {
		name += ".exe"
	}
	return name
}

// Version es la versión de agente que se distribuye.
func (r *Releases) Version() string {
	if r == nil {
		return ""
	}
	return r.version
}

// For devuelve la actualización para una plataforma y arquitectura, si existe.
// Un servidor compilado sin versión («dev») nunca ofrece actualizaciones.
func (r *Releases) For(platform, arch string) (protocol.AgentUpdate, bool) {
	if r == nil || r.version == "" || r.version == "dev" {
		return protocol.AgentUpdate{}, false
	}
	name := binaryName(platform, arch)
	sum, ok := r.sums[name]
	if !ok {
		return protocol.AgentUpdate{}, false
	}
	return protocol.AgentUpdate{Version: r.version, URL: "/downloads/" + name, SHA256: sum}, true
}

// Binaries lista los ficheros de agente disponibles.
func (r *Releases) Binaries() []string {
	out := []string{}
	if r == nil {
		return out
	}
	for name := range r.sums {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
