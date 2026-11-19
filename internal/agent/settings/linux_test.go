package settings

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mdm/internal/protocol"
)

func build(t *testing.T, f *fakeSystem, key, params string) Setting {
	t.Helper()
	s, err := Build(protocol.PlatformLinux, protocol.PolicySetting{Key: key, Mode: "enforce", Params: json.RawMessage(params)}, f.deps())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAllLinuxKeysBuild(t *testing.T) {
	f := newFake(t)
	for _, k := range append(protocol.SettingKeys["linux"], protocol.CustomScriptKey) {
		params := `{}`
		if k == "linux.forbidden_packages" {
			params = `{"names":["telnetd"]}`
		}
		if k == protocol.CustomScriptKey {
			params = `{"interpreter":"sh","detect":"x","timeout_s":60}`
		}
		if _, err := Build("linux", protocol.PolicySetting{Key: k, Mode: "audit", Params: json.RawMessage(params)}, f.deps()); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	if _, err := Build("linux", protocol.PolicySetting{Key: "linux.inventado"}, f.deps()); err == nil {
		t.Error("una clave desconocida debe dar error")
	}
}

func TestLinuxFirewall(t *testing.T) {
	f := newFake(t, "ufw")
	f.replies["ufw status"] = "Status: inactive\n"
	s := build(t, f, "linux.firewall", `{}`)
	if ok, _, err := s.Check(ctx); ok || err != nil {
		t.Fatal("ufw inactivo no cumple")
	}
	f.onCall = func(c string) {
		if c == "ufw --force enable" {
			f.replies["ufw status"] = "Status: active\n"
		}
	}
	if err := s.Apply(ctx); err != nil || !f.called("ufw --force enable") {
		t.Fatalf("aplicar: %v %v", err, f.calls)
	}
	if ok, _, _ := s.Check(ctx); !ok {
		t.Fatal("tras aplicar debe cumplir")
	}

	fd := newFake(t, "firewall-cmd")
	fd.replies["firewall-cmd --state"] = "not running\n"
	fd.fail["firewall-cmd --state"] = true
	s = build(t, fd, "linux.firewall", `{}`)
	if ok, _, err := s.Check(ctx); ok || err != nil {
		t.Fatalf("firewalld parado: %v %v", ok, err)
	}
	_ = s.Apply(ctx)
	if !fd.called("systemctl enable --now firewalld") {
		t.Fatalf("firewalld: %v", fd.calls)
	}

	none := newFake(t)
	if _, _, err := build(t, none, "linux.firewall", `{}`).Check(ctx); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("sin herramientas: %v", err)
	}
}

func TestLinuxSSH_Check(t *testing.T) {
	f := newFake(t, "sshd")
	f.replies["sshd -T"] = "port 22\npermitrootlogin yes\npasswordauthentication no\n"
	s := build(t, f, "linux.ssh", `{"root_login":false,"password_auth":false}`)
	ok, detail, err := s.Check(ctx)
	if ok || err != nil || !strings.Contains(detail, "PermitRootLogin") {
		t.Fatalf("root permitido no cumple: %v %q %v", ok, detail, err)
	}
	f.replies["sshd -T"] = "permitrootlogin prohibit-password\npasswordauthentication no\n"
	if ok, _, _ := s.Check(ctx); ok {
		t.Fatal("prohibit-password sigue permitiendo el acceso de root con clave")
	}
	f.replies["sshd -T"] = "permitrootlogin no\npasswordauthentication no\n"
	if ok, _, _ := s.Check(ctx); !ok {
		t.Fatal("debe cumplir")
	}
	if _, _, err := build(t, newFake(t), "linux.ssh", `{}`).Check(ctx); !errors.Is(err, ErrNotApplicable) {
		t.Fatal("sin sshd no aplica")
	}
}

func TestLinuxSSH_ApplyValidatesAndRollsBack(t *testing.T) {
	f := newFake(t, "sshd", "systemctl")
	f.fail["sshd -t"] = true
	f.replies["sshd -t"] = "línea 3: opción desconocida"
	s := build(t, f, "linux.ssh", `{"root_login":false,"password_auth":true}`)
	err := s.Apply(ctx)
	if err == nil || !strings.Contains(err.Error(), "opción desconocida") {
		t.Fatalf("una configuración inválida debe rechazarse: %v", err)
	}
	if f.read("etc/ssh/sshd_config.d/01-mdm.conf") != "" {
		t.Fatal("el fichero inválido debe borrarse")
	}
	f.fail["sshd -t"] = false
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.read("etc/ssh/sshd_config.d/01-mdm.conf"); !strings.Contains(got, "PermitRootLogin no") || !strings.Contains(got, "PasswordAuthentication yes") {
		t.Fatalf("fichero: %q", got)
	}
	if !f.called("systemctl reload ssh") && !f.called("systemctl reload sshd") {
		t.Fatalf("debe recargar sshd: %v", f.calls)
	}
}

func TestLinuxAutoUpdates_Debian(t *testing.T) {
	f := newFake(t, "dpkg-query", "apt-get")
	f.write("etc/debian_version", "12.7\n")
	f.replies["dpkg-query -W -f=${Status} unattended-upgrades"] = "install ok installed"
	s := build(t, f, "linux.auto_security_updates", `{}`)
	if ok, _, _ := s.Check(ctx); ok {
		t.Fatal("sin el fichero de configuración no cumple")
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.read("etc/apt/apt.conf.d/20auto-upgrades"), `APT::Periodic::Unattended-Upgrade "1";`) {
		t.Fatalf("configuración: %q", f.read("etc/apt/apt.conf.d/20auto-upgrades"))
	}
	if ok, _, _ := s.Check(ctx); !ok {
		t.Fatal("tras aplicar debe cumplir")
	}
}

func TestLinuxAutoUpdates_RHEL(t *testing.T) {
	f := newFake(t, "rpm", "dnf", "systemctl")
	f.write("etc/redhat-release", "Rocky Linux release 9.4\n")
	f.write("etc/dnf/automatic.conf", "[commands]\nupgrade_type = default\napply_updates = no\n")
	f.replies["systemctl is-enabled dnf-automatic.timer"] = "disabled"
	s := build(t, f, "linux.auto_security_updates", `{}`)
	if ok, _, _ := s.Check(ctx); ok {
		t.Fatal("no debe cumplir")
	}
	f.onCall = func(c string) {
		if c == "systemctl enable --now dnf-automatic.timer" {
			f.replies["systemctl is-enabled dnf-automatic.timer"] = "enabled"
		}
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	conf := f.read("etc/dnf/automatic.conf")
	if !strings.Contains(conf, "upgrade_type = security") || !strings.Contains(conf, "apply_updates = yes") {
		t.Fatalf("automatic.conf: %q", conf)
	}
	if ok, _, _ := s.Check(ctx); !ok {
		t.Fatal("tras aplicar debe cumplir")
	}
}

func TestDiskEncryption(t *testing.T) {
	f := newFake(t, "lsblk")
	f.replies["lsblk -J -o NAME,TYPE,MOUNTPOINTS"] = `{"blockdevices":[{"name":"sda","type":"disk","mountpoints":[null],"children":[
		{"name":"sda1","type":"part","mountpoints":["/boot"]},
		{"name":"sda2","type":"part","mountpoints":[null],"children":[{"name":"root","type":"crypt","mountpoints":["/"]}]},
		{"name":"sda3","type":"part","mountpoints":["/srv"]}]}]}`
	s := build(t, f, "linux.disk_encryption", `{}`)
	ok, detail, _ := s.Check(ctx)
	if ok || !strings.Contains(detail, "/srv") || strings.Contains(detail, "/boot") {
		t.Fatalf("un volumen de datos sin cifrar no cumple; /boot no cuenta: %v %q", ok, detail)
	}
	if err := s.Apply(ctx); err == nil {
		t.Fatal("solo auditable: Apply debe fallar")
	}
}

func TestForbiddenPackages(t *testing.T) {
	f := newFake(t, "dpkg-query")
	f.replies["dpkg-query -W -f=${Package} ${Status}\n telnetd nis rsh-server"] = "telnetd install ok installed\nnis deinstall ok config-files\n"
	s := build(t, f, "linux.forbidden_packages", `{"names":["telnetd","nis","rsh-server"]}`)
	ok, detail, err := s.Check(ctx)
	if ok || err != nil || detail != "Instalados: telnetd." {
		t.Fatalf("%v %q %v", ok, detail, err)
	}
}

func TestCustomScript(t *testing.T) {
	f := newFake(t)
	f.replies["exec detectar"] = "1"
	s := build(t, f, protocol.CustomScriptKey, `{"interpreter":"sh","detect":"detectar","remediate":"corregir","timeout_s":30}`)
	ok, detail, _ := s.Check(ctx)
	if ok || detail != "detalle de detectar" {
		t.Fatalf("%v %q", ok, detail)
	}
	if err := s.Apply(ctx); err != nil || !f.called("exec corregir") {
		t.Fatalf("corrección: %v %v", err, f.calls)
	}
	f.replies["exec detectar"] = "0"
	if ok, _, _ := s.Check(ctx); !ok {
		t.Fatal("detección 0 cumple")
	}
	noFix := build(t, f, protocol.CustomScriptKey, `{"interpreter":"sh","detect":"detectar","timeout_s":30}`)
	if err := noFix.Apply(ctx); err == nil {
		t.Fatal("sin script de corrección no se puede aplicar")
	}
}
