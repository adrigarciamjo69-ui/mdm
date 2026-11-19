// Package service integra el agente con el gestor de servicios del sistema:
// systemd en Linux y el Administrador de control de servicios en Windows.
package service

import (
	"fmt"
	"strings"
)

// unitFile devuelve la unidad de systemd del agente. Se ejecuta como root
// porque necesita leer el inventario completo, pero con /usr, /boot y /etc en
// solo lectura salvo su propio directorio de configuración. No se usa
// ProtectSystem=full porque dejaría /run en solo lectura, y iptables (que
// usa «ufw status») necesita escribir allí su cerrojo.
func unitFile(binPath string) string {
	exec := binPath
	if strings.ContainsAny(binPath, " \t") {
		exec = `"` + binPath + `"`
	}
	return fmt.Sprintf(`[Unit]
Description=Agente MDM
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run
User=root
Restart=always
RestartSec=10
NoNewPrivileges=true
ProtectSystem=full
ReadWritePaths=/etc/mdm-agent
PrivateTmp=true

[Install]
WantedBy=multi-user.target
`, exec)
}
