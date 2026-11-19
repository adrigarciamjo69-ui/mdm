// Package config guarda la configuración y el estado del agente en disco, en
// un directorio legible solo por el administrador del equipo.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config es lo que el agente necesita para hablar con el servidor.
type Config struct {
	ServerURL          string `json:"server_url"`
	DeviceID           string `json:"device_id"`
	Credential         string `json:"credential"`
	CheckinIntervalS   int    `json:"checkin_interval_s"`
	InventoryIntervalS int    `json:"inventory_interval_s"`
	// Revoked indica que el servidor rechazó la credencial: el agente queda en
	// espera hasta que se vuelva a inscribir.
	Revoked bool `json:"revoked"`
}

// Bearer devuelve la credencial tal como se envía en la cabecera Authorization.
func (c Config) Bearer() string { return c.DeviceID + "." + c.Credential }

// State resume la última actividad del agente, para la orden «status».
type State struct {
	LastSuccess *time.Time `json:"last_success"`
	LastError   string     `json:"last_error"`
	LastErrorAt *time.Time `json:"last_error_at"`
}

// Path es la ubicación del fichero de configuración.
func Path() string { return filepath.Join(Dir(), "agent.json") }

// StatePath es la ubicación del fichero de estado.
func StatePath() string { return filepath.Join(Dir(), "state.json") }

// Load lee la configuración. Si el fichero no existe, el error cumple
// errors.Is(err, fs.ErrNotExist).
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("la configuración del agente (%s) está dañada: %w", path, err)
	}
	return c, nil
}

// Save escribe la configuración de forma atómica y restringe el acceso al
// directorio: contiene la credencial del equipo.
func Save(path string, c Config) error { return writeJSON(path, c) }

// LoadState lee el estado. Un fichero inexistente o dañado equivale a un
// estado vacío: es solo informativo.
func LoadState(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, nil
	}
	return s, nil
}

func SaveState(path string, s State) error { return writeJSON(path, s) }

// Remove borra el directorio de configuración con todo su contenido.
func Remove(dir string) error { return os.RemoveAll(dir) }

// restricted recuerda los directorios ya protegidos en este proceso. El estado
// se guarda cada minuto y en Windows proteger el directorio supone lanzar
// icacls: basta hacerlo una vez por arranque.
var restricted sync.Map

func restrictOnce(dir string) error {
	if _, done := restricted.Load(dir); done {
		return nil
	}
	if err := restrict(dir); err != nil {
		return err
	}
	restricted.Store(dir, true)
	return nil
}

// writeJSON escribe en un fichero temporal del mismo directorio y lo renombra,
// para que un corte de luz nunca deje un fichero a medias.
func writeJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crear %s: %w", dir, err)
	}
	if err := restrictOnce(dir); err != nil {
		return fmt.Errorf("proteger %s: %w", dir, err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("escribir en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no hace nada si el renombrado tuvo éxito
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
