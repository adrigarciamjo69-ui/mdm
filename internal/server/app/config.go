// Package app ensambla el servidor: configuración, rutas, consola estática,
// descargas del agente y limpieza periódica.
package app

import (
	"errors"
	"fmt"
	"strings"

	"mdm/internal/server/secrets"
)

type Config struct {
	DatabaseURL  string
	ListenAddr   string
	PublicURL    string
	DownloadsDir string
	// FilesDir guarda los instaladores subidos (volumen persistente).
	FilesDir string
	// AndroidAPIBase sustituye la dirección de la Android Management API (solo pruebas).
	AndroidAPIBase string
	// EntraAuthBase y GraphBase sustituyen las direcciones de Microsoft (solo pruebas).
	EntraAuthBase string
	GraphBase     string
	// TrustProxy indica que hay un proxy inverso delante: la IP del cliente se
	// toma del último valor de X-Forwarded-For.
	TrustProxy bool
	// SecretKey cifra los datos sensibles guardados en la base de datos.
	SecretKey string
	// Version es la versión del servidor, que es también la del agente que distribuye.
	Version string
}

// LoadConfig lee la configuración de las variables de entorno MDM_*.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{
		DatabaseURL:    getenv("MDM_DATABASE_URL"),
		ListenAddr:     getenv("MDM_LISTEN_ADDR"),
		PublicURL:      strings.TrimRight(getenv("MDM_PUBLIC_URL"), "/"),
		DownloadsDir:   getenv("MDM_DOWNLOADS_DIR"),
		FilesDir:       getenv("MDM_FILES_DIR"),
		AndroidAPIBase: getenv("MDM_ANDROID_API_BASE"),
		EntraAuthBase:  getenv("MDM_ENTRA_AUTH_BASE"),
		GraphBase:      getenv("MDM_GRAPH_BASE"),
		TrustProxy:     strings.EqualFold(getenv("MDM_TRUSTED_PROXY"), "true"),
		SecretKey:      getenv("MDM_SECRET_KEY"),
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("falta MDM_DATABASE_URL: la cadena de conexión de PostgreSQL")
	}
	if c.PublicURL == "" {
		return Config{}, errors.New("falta MDM_PUBLIC_URL: la dirección pública del servidor, por ejemplo https://mdm.ejemplo.es")
	}
	if !strings.HasPrefix(c.PublicURL, "https://") {
		return Config{}, errors.New("MDM_PUBLIC_URL debe empezar por https://: los agentes solo aceptan conexiones cifradas")
	}
	if c.SecretKey == "" {
		return Config{}, errors.New("falta MDM_SECRET_KEY: genera una con «openssl rand -base64 32» y guárdala con las copias de seguridad")
	}
	if _, err := secrets.New(c.SecretKey); err != nil {
		return Config{}, fmt.Errorf("%w", err)
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	if c.FilesDir == "" {
		c.FilesDir = "/data/files"
	}
	if c.DownloadsDir == "" {
		c.DownloadsDir = "/downloads"
	}
	return c, nil
}
