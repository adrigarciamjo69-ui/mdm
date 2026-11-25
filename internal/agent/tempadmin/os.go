package tempadmin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Runner ejecuta una orden del sistema (sin intérprete de órdenes: cada
// argumento llega tal cual) y devuelve su salida.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func run(ctx context.Context, r Runner, name string, args ...string) (string, error) {
	out, err := r(ctx, name, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return string(out), fmt.Errorf("%s: %w", name, err)
		}
		return string(out), fmt.Errorf("%s: %s", name, lastLine(msg))
	}
	return string(out), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Windows usa el grupo local de administradores por su SID (S-1-5-32-544),
// cuyo nombre depende del idioma, y net.exe para añadir y quitar.
type Windows struct {
	Run   Runner
	group string
}

func (w *Windows) powershell(ctx context.Context, command string) (string, error) {
	return run(ctx, w.Run, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
}

func (w *Windows) adminGroup(ctx context.Context) (string, error) {
	if w.group != "" {
		return w.group, nil
	}
	out, err := w.powershell(ctx, "(New-Object System.Security.Principal.SecurityIdentifier('S-1-5-32-544')).Translate([System.Security.Principal.NTAccount]).Value")
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return "", errors.New("no se encuentra el grupo local de administradores")
	}
	w.group = name
	return name, nil
}

func (w *Windows) CurrentUser(ctx context.Context) (string, error) {
	out, err := w.powershell(ctx, "(Get-CimInstance Win32_ComputerSystem).UserName")
	if err != nil {
		return "", err
	}
	u := strings.TrimSpace(out)
	if u == "" {
		return "", errors.New("no hay ningún usuario con la sesión iniciada")
	}
	return u, nil
}

func (w *Windows) members(ctx context.Context) ([]string, error) {
	g, err := w.adminGroup(ctx)
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, w.Run, "net.exe", "localgroup", g)
	if err != nil {
		return nil, err
	}
	return parseNetLocalgroup(out), nil
}

func (w *Windows) IsAdmin(ctx context.Context, user string) (bool, error) {
	m, err := w.members(ctx)
	if err != nil {
		return false, err
	}
	return memberMatches(m, user), nil
}

func (w *Windows) Add(ctx context.Context, user string) error {
	g, err := w.adminGroup(ctx)
	if err != nil {
		return err
	}
	_, err = run(ctx, w.Run, "net.exe", "localgroup", g, user, "/add")
	return err
}

func (w *Windows) Remove(ctx context.Context, user string) error {
	m, err := w.members(ctx)
	if err != nil {
		return err
	}
	if !memberMatches(m, user) {
		return nil
	}
	_, err = run(ctx, w.Run, "net.exe", "localgroup", w.group, user, "/delete")
	return err
}

// parseNetLocalgroup extrae los miembros de la salida de «net localgroup»:
// las líneas entre la de guiones y el mensaje final, en cualquier idioma.
func parseNetLocalgroup(out string) []string {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "---") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var members []string
	for _, l := range lines[start:] {
		if l = strings.TrimSpace(l); l != "" {
			members = append(members, l)
		}
	}
	if len(members) > 0 {
		members = members[:len(members)-1] // «Se ha completado el comando correctamente.»
	}
	return members
}

func splitAccount(s string) (domain, name string) {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, `\`); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// memberMatches compara sin distinguir mayúsculas. Un usuario sin dominio
// coincide con cualquier miembro de ese nombre; con dominio, solo con el
// mismo dominio.
func memberMatches(members []string, user string) bool {
	ud, un := splitAccount(user)
	for _, m := range members {
		md, mn := splitAccount(m)
		if mn != un {
			continue
		}
		if ud == "" || ud == md {
			return true
		}
	}
	return false
}

var errNoUser = errors.New("usuario inexistente")

// Linux usa el grupo sudo (Debian, Ubuntu) o wheel (Fedora, RHEL).
type Linux struct {
	Run Runner
	// Root es la raíz del sistema de ficheros («/» en un equipo real).
	Root string
}

func (l *Linux) adminGroup() (string, error) {
	b, err := os.ReadFile(filepath.Join(l.Root, "etc/group"))
	if err != nil {
		return "", err
	}
	has := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.IndexByte(line, ':'); i > 0 {
			has[line[:i]] = true
		}
	}
	for _, g := range []string{"sudo", "wheel"} {
		if has[g] {
			return g, nil
		}
	}
	return "", errors.New("este sistema no tiene grupo sudo ni wheel")
}

func (l *Linux) CurrentUser(ctx context.Context) (string, error) {
	out, err := run(ctx, l.Run, "who")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] != "root" {
			return f[0], nil
		}
	}
	return "", errors.New("no hay ningún usuario con la sesión iniciada")
}

func (l *Linux) IsAdmin(ctx context.Context, user string) (bool, error) {
	g, err := l.adminGroup()
	if err != nil {
		return false, err
	}
	out, err := l.Run(ctx, "id", "-nG", "--", user)
	if err != nil {
		return false, fmt.Errorf("el usuario %s no existe en este equipo: %w", user, errNoUser)
	}
	for _, x := range strings.Fields(string(out)) {
		if x == g {
			return true, nil
		}
	}
	return user == "root", nil
}

func (l *Linux) Add(ctx context.Context, user string) error {
	g, err := l.adminGroup()
	if err != nil {
		return err
	}
	_, err = run(ctx, l.Run, "usermod", "-aG", g, user)
	return err
}

func (l *Linux) Remove(ctx context.Context, user string) error {
	ok, err := l.IsAdmin(ctx, user)
	if errors.Is(err, errNoUser) {
		return nil // borrado: ya no es administrador
	}
	if err != nil || !ok {
		return err
	}
	g, _ := l.adminGroup()
	_, err = run(ctx, l.Run, "gpasswd", "-d", user, g)
	return err
}
