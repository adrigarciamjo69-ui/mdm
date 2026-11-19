package auth

import (
	"fmt"

	"github.com/pquerna/otp/totp"
)

const totpIssuer = "MDM"

// NewTOTP genera un secreto TOTP y la URI otpauth:// para el código QR.
func NewTOTP(email string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: email})
	if err != nil {
		return "", "", fmt.Errorf("generar TOTP: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP valida un código de 6 dígitos con la tolerancia habitual de ±30 s.
func VerifyTOTP(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	return totp.Validate(code, secret)
}
