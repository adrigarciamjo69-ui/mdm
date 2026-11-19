package app

import (
	"strings"
	"testing"
)

// testSecretKey son 32 bytes en base64, válidos para las pruebas.
const testSecretKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestLoadConfig_RequiresSecretKey(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	base := map[string]string{"MDM_DATABASE_URL": "postgres://x", "MDM_PUBLIC_URL": "https://x.es"}
	if _, err := LoadConfig(env(base)); err == nil || !strings.Contains(err.Error(), "MDM_SECRET_KEY") {
		t.Fatalf("sin clave: %v", err)
	}
	base["MDM_SECRET_KEY"] = "corta"
	if _, err := LoadConfig(env(base)); err == nil || !strings.Contains(err.Error(), "MDM_SECRET_KEY") {
		t.Fatalf("clave inválida: %v", err)
	}
	base["MDM_SECRET_KEY"] = testSecretKey
	if c, err := LoadConfig(env(base)); err != nil || c.SecretKey != testSecretKey {
		t.Fatalf("clave válida: %v", err)
	}
}
