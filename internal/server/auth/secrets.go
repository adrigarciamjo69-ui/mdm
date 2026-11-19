package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// NewSecret genera un secreto de 256 bits. Devuelve su forma en claro (que se
// entrega una sola vez) y el hash que se guarda.
func NewSecret() (plain, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generar secreto: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashSecret(plain), nil
}

// HashSecret devuelve el SHA-256 en hexadecimal. Basta un hash rápido porque
// los secretos son aleatorios de alta entropía, no contraseñas.
func HashSecret(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// SecretMatches compara en tiempo constante. Un hash o un secreto vacío nunca coincide.
func SecretMatches(hash, plain string) bool {
	if hash == "" || plain == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(HashSecret(plain))) == 1
}

// ConstantTimeEqual compara dos cadenas en tiempo constante. Dos cadenas
// vacías no se consideran iguales: un token ausente nunca coincide.
func ConstantTimeEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
