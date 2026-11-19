package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

func TestPassword_RoundTrip(t *testing.T) {
	h, err := HashPassword("correcta-y-larga-1")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "correcta-y-larga-1") {
		t.Error("la contraseña correcta no verifica")
	}
	if VerifyPassword(h, "correcta-y-larga-2") {
		t.Error("una contraseña distinta verifica")
	}
}

func TestPassword_HashFormat(t *testing.T) {
	h, _ := HashPassword("correcta-y-larga-1")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("formato inesperado: %s", h)
	}
}

func TestPassword_SaltDiffers(t *testing.T) {
	a, _ := HashPassword("correcta-y-larga-1")
	b, _ := HashPassword("correcta-y-larga-1")
	if a == b {
		t.Fatal("dos hashes iguales: la sal no es aleatoria")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	for _, h := range []string{"", "texto", "$argon2id$v=19$m=65536,t=3,p=2$mal", "$argon2id$v=19$m=65536,t=3,p=2$!!$!!"} {
		if VerifyPassword(h, "x") {
			t.Errorf("hash mal formado %q verifica", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("12345678901") == nil {
		t.Error("11 caracteres deberían rechazarse")
	}
	if err := ValidatePassword("123456789012"); err != nil {
		t.Errorf("12 caracteres deberían aceptarse: %v", err)
	}
	if err := ValidatePassword("ñandú-ñandú-"); err != nil {
		t.Errorf("12 caracteres no ASCII deberían aceptarse: %v", err)
	}
}

func TestNewSecret(t *testing.T) {
	plain, hash, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil || len(raw) != 32 {
		t.Fatalf("el secreto no son 32 bytes en base64url: %v (%d)", err, len(raw))
	}
	if HashSecret(plain) != hash {
		t.Error("HashSecret(plain) no coincide con el hash devuelto")
	}
	if len(hash) != 64 {
		t.Errorf("el hash no es SHA-256 en hexadecimal: %s", hash)
	}
	other, _, _ := NewSecret()
	if other == plain {
		t.Error("dos secretos iguales")
	}
}

func TestSecretMatches(t *testing.T) {
	plain, hash, _ := NewSecret()
	other, _, _ := NewSecret()
	if !SecretMatches(hash, plain) {
		t.Error("el secreto propio no coincide")
	}
	if SecretMatches(hash, other) || SecretMatches(hash, "") || SecretMatches("", "") {
		t.Error("coincide un secreto que no debería")
	}
}

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		r, min Role
		want   bool
	}{
		{RoleAdmin, RoleOperator, true}, {RoleOperator, RoleAdmin, false},
		{RoleReadonly, RoleReadonly, true}, {RoleReadonly, RoleOperator, false},
		{RoleAdmin, RoleAdmin, true}, {RoleOperator, RoleReadonly, true},
		{Role("root"), RoleReadonly, false}, {RoleAdmin, Role("root"), false},
	}
	for _, c := range cases {
		if got := c.r.AtLeast(c.min); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v, se esperaba %v", c.r, c.min, got, c.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	if r, err := ParseRole("admin"); err != nil || r != RoleAdmin {
		t.Errorf("admin: %v %v", r, err)
	}
	if _, err := ParseRole("root"); err == nil {
		t.Error("root debería rechazarse")
	}
}

func TestTOTP(t *testing.T) {
	secret, uri, err := NewTOTP("ana@ejemplo.es")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uri, "issuer=MDM") || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Errorf("uri inesperada: %s", uri)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(secret, code) {
		t.Error("un código válido no verifica")
	}
	bad := "000000"
	if code == bad {
		bad = "111111"
	}
	if VerifyTOTP(secret, bad) || VerifyTOTP(secret, "") || VerifyTOTP("", code) {
		t.Error("verifica un código que no debería")
	}
}

func TestDeviceCredential(t *testing.T) {
	id := uuid.New()
	secret, _, _ := NewSecret()
	gotID, gotSecret, err := ParseDeviceCredential("Bearer " + FormatDeviceCredential(id.String(), secret))
	if err != nil || gotID != id || gotSecret != secret {
		t.Fatalf("ida y vuelta: %v %v %v", gotID, gotSecret, err)
	}
	for _, h := range []string{"", FormatDeviceCredential(id.String(), secret), "Bearer " + id.String(), "Bearer no-uuid.secreto", "Bearer " + id.String() + ".", "Basic abc"} {
		if _, _, err := ParseDeviceCredential(h); err == nil {
			t.Errorf("%q debería rechazarse", h)
		}
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("abc", "abc") {
		t.Error("iguales no coinciden")
	}
	if ConstantTimeEqual("abc", "abd") || ConstantTimeEqual("", "") || ConstantTimeEqual("abc", "") {
		t.Error("coinciden cadenas que no deberían")
	}
}
