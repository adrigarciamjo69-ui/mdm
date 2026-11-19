// Package secrets cifra los datos sensibles que el servidor guarda en la base
// de datos (claves de recuperación, credenciales de Google, contraseña SMTP…)
// con la clave MDM_SECRET_KEY.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const prefix = "v1:"

// ErrUndecryptable indica que un valor no se puede descifrar con la clave
// actual: está dañado o se cifró con otra clave.
var ErrUndecryptable = errors.New("no se puede descifrar con la clave actual del servidor")

// Box cifra y descifra con AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// New crea la caja a partir de la clave en base64 (32 bytes).
func New(keyB64 string) (*Box, error) {
	const hint = "debe ser de 32 bytes en base64; genera una con «openssl rand -base64 32»"
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("MDM_SECRET_KEY no es válida: %s", hint)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal cifra plain con un nonce aleatorio.
func (b *Box) Seal(plain []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generar nonce: %w", err)
	}
	out := b.aead.Seal(nonce, nonce, plain, nil)
	return prefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open descifra un valor de Seal. Cualquier problema devuelve ErrUndecryptable.
func (b *Box) Open(sealed string) ([]byte, error) {
	rest, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return nil, ErrUndecryptable
	}
	raw, err := base64.StdEncoding.DecodeString(rest)
	n := b.aead.NonceSize()
	if err != nil || len(raw) < n+b.aead.Overhead() {
		return nil, ErrUndecryptable
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return nil, ErrUndecryptable
	}
	return plain, nil
}
