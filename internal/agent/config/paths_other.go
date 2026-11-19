//go:build !windows

package config

// Dir es el directorio de configuración del agente.
func Dir() string { return "/etc/mdm-agent" }
