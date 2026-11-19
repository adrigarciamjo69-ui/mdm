package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"mdm/internal/agent/inventory"
	"mdm/internal/protocol"
)

func init() {
	l := protocol.PlatformLinux
	register(l, "linux.firewall", func(_ json.RawMessage, d Deps) (Setting, error) { return &linuxFirewall{d}, nil })
	register(l, "linux.auto_security_updates", func(_ json.RawMessage, d Deps) (Setting, error) { return &autoUpdates{d}, nil })
	register(l, "linux.ssh", func(p json.RawMessage, d Deps) (Setting, error) {
		s := &sshSetting{d: d}
		return s, json.Unmarshal(p, &s.p)
	})
	register(l, "linux.disk_encryption", func(_ json.RawMessage, d Deps) (Setting, error) { return &diskEncryption{d}, nil })
	register(l, "linux.forbidden_packages", func(p json.RawMessage, d Deps) (Setting, error) {
		s := &forbiddenPackages{d: d}
		return s, json.Unmarshal(p, &s.p)
	})
}

var errAuditOnly = errors.New("este ajuste solo se puede auditar")

// ---------- Cortafuegos ----------

type linuxFirewall struct{ d Deps }

func (f *linuxFirewall) Check(ctx context.Context) (bool, string, error) {
	switch {
	case f.d.Has("ufw"):
		out, _ := f.d.Run(ctx, "ufw", "status")
		on := strings.Contains(string(out), "Status: active")
		return on, "ufw: " + lastLine(string(out)), nil
	case f.d.Has("firewall-cmd"):
		out, _ := f.d.Run(ctx, "firewall-cmd", "--state")
		state := strings.TrimSpace(string(out))
		return state == "running", "firewalld: " + state, nil
	}
	return false, "No hay ufw ni firewalld.", ErrNotApplicable
}

func (f *linuxFirewall) Apply(ctx context.Context) error {
	if f.d.Has("ufw") {
		_, err := f.d.run(ctx, "ufw", "--force", "enable")
		return err
	}
	_, err := f.d.run(ctx, "systemctl", "enable", "--now", "firewalld")
	return err
}

// ---------- Actualizaciones de seguridad automáticas ----------

type autoUpdates struct{ d Deps }

const (
	aptAutoFile  = "etc/apt/apt.conf.d/20auto-upgrades"
	aptAutoConf  = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	dnfAutoFile  = "etc/dnf/automatic.conf"
	dnfAutoTimer = "dnf-automatic.timer"
)

func (a *autoUpdates) debian() bool { return a.d.exists("etc/debian_version") }
func (a *autoUpdates) rhel() bool   { return a.d.exists("etc/redhat-release") }

func (a *autoUpdates) Check(ctx context.Context) (bool, string, error) {
	switch {
	case a.debian():
		out, _ := a.d.Run(ctx, "dpkg-query", "-W", "-f=${Status}", "unattended-upgrades")
		if !strings.Contains(string(out), "install ok installed") {
			return false, "unattended-upgrades no está instalado.", nil
		}
		if !strings.Contains(a.d.readFile(aptAutoFile), `APT::Periodic::Unattended-Upgrade "1"`) {
			return false, "unattended-upgrades está instalado pero no activado.", nil
		}
		return true, "unattended-upgrades activo.", nil
	case a.rhel():
		if out, err := a.d.Run(ctx, "rpm", "-q", "dnf-automatic"); err != nil {
			return false, "dnf-automatic no está instalado: " + lastLine(string(out)), nil
		}
		conf := a.d.readFile(dnfAutoFile)
		if !regexp.MustCompile(`(?m)^\s*upgrade_type\s*=\s*security\s*$`).MatchString(conf) ||
			!regexp.MustCompile(`(?m)^\s*apply_updates\s*=\s*yes\s*$`).MatchString(conf) {
			return false, "dnf-automatic no está configurado para instalar actualizaciones de seguridad.", nil
		}
		out, _ := a.d.Run(ctx, "systemctl", "is-enabled", dnfAutoTimer)
		if strings.TrimSpace(string(out)) != "enabled" {
			return false, "El temporizador de dnf-automatic no está activado.", nil
		}
		return true, "dnf-automatic activo para seguridad.", nil
	}
	return false, "Distribución no reconocida.", ErrNotApplicable
}

// setConfValue fija «clave = valor» en un fichero INI sencillo, sustituyendo
// la línea existente o añadiéndola.
func setConfValue(conf, key, value string) string {
	re := regexp.MustCompile(`(?m)^\s*#?\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	line := key + " = " + value
	if re.MatchString(conf) {
		return re.ReplaceAllLiteralString(conf, line)
	}
	if conf != "" && !strings.HasSuffix(conf, "\n") {
		conf += "\n"
	}
	return conf + line + "\n"
}

func (a *autoUpdates) Apply(ctx context.Context) error {
	switch {
	case a.debian():
		if _, err := a.d.run(ctx, "apt-get", "install", "-y", "unattended-upgrades"); err != nil {
			return err
		}
		return a.d.writeFile(aptAutoFile, aptAutoConf, 0o644)
	case a.rhel():
		if _, err := a.d.run(ctx, "dnf", "install", "-y", "dnf-automatic"); err != nil {
			return err
		}
		conf := a.d.readFile(dnfAutoFile)
		conf = setConfValue(conf, "upgrade_type", "security")
		conf = setConfValue(conf, "apply_updates", "yes")
		if err := a.d.writeFile(dnfAutoFile, conf, 0o644); err != nil {
			return err
		}
		_, err := a.d.run(ctx, "systemctl", "enable", "--now", dnfAutoTimer)
		return err
	}
	return ErrNotApplicable
}

// ---------- SSH ----------

type sshParams struct {
	RootLogin    bool `json:"root_login"`
	PasswordAuth bool `json:"password_auth"`
}

type sshSetting struct {
	d Deps
	p sshParams
}

// sshDropIn empieza por 01 porque en sshd gana el primer valor leído y los
// ficheros del directorio se leen en orden alfabético (p. ej. antes que
// 50-cloud-init.conf).
const sshDropIn = "etc/ssh/sshd_config.d/01-mdm.conf"

func (s *sshSetting) Check(ctx context.Context) (bool, string, error) {
	if !s.d.Has("sshd") {
		return false, "No hay servidor SSH.", ErrNotApplicable
	}
	out, err := s.d.Run(ctx, "sshd", "-T")
	if err != nil {
		return false, "", fmt.Errorf("sshd -T: %w", err)
	}
	vals := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			vals[strings.ToLower(k)] = strings.ToLower(strings.TrimSpace(v))
		}
	}
	var problems []string
	root := vals["permitrootlogin"]
	if !s.p.RootLogin && root != "no" {
		problems = append(problems, "PermitRootLogin es «"+root+"» y debe ser «no»")
	}
	if s.p.RootLogin && root == "no" {
		problems = append(problems, "PermitRootLogin es «no» y debe permitirse")
	}
	pass := vals["passwordauthentication"]
	if want := yesNo(s.p.PasswordAuth); pass != want {
		problems = append(problems, "PasswordAuthentication es «"+pass+"» y debe ser «"+want+"»")
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; ") + ".", nil
	}
	return true, "Configuración de SSH correcta.", nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (s *sshSetting) Apply(ctx context.Context) error {
	root := "no"
	if s.p.RootLogin {
		root = "prohibit-password"
	}
	content := "# Gestionado por MDM. Los cambios manuales se sobrescriben.\n" +
		"PermitRootLogin " + root + "\nPasswordAuthentication " + yesNo(s.p.PasswordAuth) + "\n"
	if err := s.d.writeFile(sshDropIn, content, 0o644); err != nil {
		return err
	}
	if _, err := s.d.run(ctx, "sshd", "-t"); err != nil {
		_ = os.Remove(s.d.path(sshDropIn))
		return fmt.Errorf("la configuración de SSH no es válida y no se ha aplicado: %w", err)
	}
	if _, err := s.d.run(ctx, "systemctl", "reload", "ssh"); err != nil {
		_, err = s.d.run(ctx, "systemctl", "reload", "sshd")
		return err
	}
	return nil
}

// ---------- Cifrado de disco ----------

type diskEncryption struct{ d Deps }

var notData = map[string]bool{"/boot": true, "/boot/efi": true, "[SWAP]": true}

func (e *diskEncryption) Check(ctx context.Context) (bool, string, error) {
	if !e.d.Has("lsblk") {
		return false, "", ErrNotApplicable
	}
	out, err := e.d.Run(ctx, "lsblk", "-J", "-o", "NAME,TYPE,MOUNTPOINTS")
	if err != nil {
		return false, "", fmt.Errorf("lsblk: %w", err)
	}
	m, err := inventory.CryptMounts(out)
	if err != nil {
		return false, "", err
	}
	var plain []string
	for mount, enc := range m {
		if !enc && !notData[mount] && !strings.HasPrefix(mount, "/snap/") {
			plain = append(plain, mount)
		}
	}
	sort.Strings(plain)
	if len(plain) > 0 {
		return false, "Sin cifrar: " + strings.Join(plain, ", ") + ".", nil
	}
	return true, "Todos los volúmenes de datos están cifrados.", nil
}

func (e *diskEncryption) Apply(context.Context) error { return errAuditOnly }

// ---------- Paquetes prohibidos ----------

type forbiddenPackages struct {
	d Deps
	p struct {
		Names []string `json:"names"`
	}
}

func (f *forbiddenPackages) Check(ctx context.Context) (bool, string, error) {
	var found []string
	switch {
	case f.d.Has("dpkg-query"):
		args := append([]string{"-W", "-f=${Package} ${Status}\n"}, f.p.Names...)
		out, _ := f.d.Run(ctx, "dpkg-query", args...) // termina con error si alguno no existe
		for _, line := range strings.Split(string(out), "\n") {
			if name, status, ok := strings.Cut(line, " "); ok && strings.Contains(status, "install ok installed") {
				found = append(found, name)
			}
		}
	case f.d.Has("rpm"):
		for _, n := range f.p.Names {
			if _, err := f.d.Run(ctx, "rpm", "-q", n); err == nil {
				found = append(found, n)
			}
		}
	default:
		return false, "", ErrNotApplicable
	}
	if len(found) > 0 {
		return false, "Instalados: " + strings.Join(found, ", ") + ".", nil
	}
	return true, "Ninguno instalado.", nil
}

func (f *forbiddenPackages) Apply(context.Context) error { return errAuditOnly }
