package service

import (
	"strings"
	"testing"
)

func TestUnitFile(t *testing.T) {
	u := unitFile("/usr/local/bin/mdm-agent")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/mdm-agent run", "Restart=always", "RestartSec=10",
		"After=network-online.target", "Wants=network-online.target", "WantedBy=multi-user.target",
		"User=root", "ProtectSystem=full", "ReadWritePaths=/etc/mdm-agent", "NoNewPrivileges=true",
		"[Unit]", "[Service]", "[Install]", "Description=",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("falta %q en la unidad:\n%s", want, u)
		}
	}
	if !strings.HasSuffix(u, "\n") {
		t.Error("la unidad debe terminar en salto de línea")
	}
}

func TestUnitFile_PathWithSpacesIsQuoted(t *testing.T) {
	u := unitFile("/opt/mi agente/mdm-agent")
	if !strings.Contains(u, `ExecStart="/opt/mi agente/mdm-agent" run`) {
		t.Errorf("ruta con espacios sin entrecomillar:\n%s", u)
	}
}

func TestUnitFile_RunStaysWritable(t *testing.T) {
	// iptables (que usa «ufw status») escribe su cerrojo en /run: con
	// ProtectSystem=strict no podría y el cortafuegos saldría como desconocido.
	if strings.Contains(unitFile("/usr/local/bin/mdm-agent"), "ProtectSystem=strict") {
		t.Fatal("ProtectSystem=strict deja /run en solo lectura")
	}
}
