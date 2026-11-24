package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCLI_NoArgsAndUnknown(t *testing.T) {
	if code, _, errOut := runCLI(t, ""); code != 1 || !strings.Contains(errOut, "Uso:") {
		t.Errorf("sin argumentos: %d %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "", "bailar"); code != 1 || !strings.Contains(errOut, "Orden desconocida") {
		t.Errorf("orden desconocida: %d %s", code, errOut)
	}
	if code, out, _ := runCLI(t, "", "version"); code != 0 || strings.TrimSpace(out) != version {
		t.Errorf("version: %d %q", code, out)
	}
}

func TestCLI_AdminCreateAndResetTOTP(t *testing.T) {
	s, url := storetest.NewWithURL(t)
	t.Setenv("MDM_DATABASE_URL", url)
	ctx := context.Background()

	if code, _, errOut := runCLI(t, "corta\n", "admin", "create", "--email", "ana@ejemplo.es"); code != 1 || !strings.Contains(errOut, "12 caracteres") {
		t.Fatalf("contraseña corta: %d %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "correcta-y-larga-1\n", "admin", "create", "--email", "ana@ejemplo.es", "--role", "root"); code != 1 || !strings.Contains(errOut, "rol") {
		t.Fatalf("rol inválido: %d %s", code, errOut)
	}
	code, out, errOut := runCLI(t, "correcta-y-larga-1\n", "admin", "create", "--email", "ana@ejemplo.es")
	if code != 0 || !strings.Contains(out, "ana@ejemplo.es") {
		t.Fatalf("crear: %d %s %s", code, out, errOut)
	}
	a, err := s.AdminByEmail(ctx, "ana@ejemplo.es")
	if err != nil || a.Role != "admin" || !auth.VerifyPassword(a.PasswordHash, "correcta-y-larga-1") {
		t.Fatalf("administrador creado: %+v %v", a, err)
	}
	if code, _, errOut := runCLI(t, "correcta-y-larga-1\n", "admin", "create", "--email", "ANA@ejemplo.es"); code != 1 || !strings.Contains(errOut, "ya existe") {
		t.Fatalf("duplicado: %d %s", code, errOut)
	}

	secret := "JBSWY3DPEHPK3PXP"
	_ = s.SetAdminTOTP(ctx, a.ID, &secret)
	_ = s.CreateSession(ctx, store.Session{TokenHash: "h", AdminID: a.ID, CSRFToken: "c", CreatedAt: a.CreatedAt, ExpiresAt: a.CreatedAt.AddDate(1, 0, 0)})
	if code, _, errOut := runCLI(t, "", "admin", "reset-totp", "--email", "ana@ejemplo.es"); code != 0 {
		t.Fatalf("reset-totp: %d %s", code, errOut)
	}
	a, _ = s.AdminByEmail(ctx, "ana@ejemplo.es")
	if a.TOTPSecret != nil {
		t.Error("el segundo factor no se borró")
	}
	if _, _, err := s.SessionByTokenHash(ctx, "h", a.CreatedAt); err != store.ErrNotFound {
		t.Errorf("las sesiones no se cerraron: %v", err)
	}
	if code, _, errOut := runCLI(t, "", "admin", "reset-totp", "--email", "nadie@ejemplo.es"); code != 1 || !strings.Contains(errOut, "no existe") {
		t.Errorf("reset-totp de un correo inexistente: %d %s", code, errOut)
	}
}

func TestCLI_AdminResetPassword(t *testing.T) {
	s, url := storetest.NewWithURL(t)
	t.Setenv("MDM_DATABASE_URL", url)
	ctx := context.Background()
	if code, _, errOut := runCLI(t, "correcta-y-larga-1\n", "admin", "create", "--email", "ana@ejemplo.es"); code != 0 {
		t.Fatalf("crear: %d %s", code, errOut)
	}
	a, _ := s.AdminByEmail(ctx, "ana@ejemplo.es")
	_ = s.CreateSession(ctx, store.Session{TokenHash: "h", AdminID: a.ID, CSRFToken: "c", CreatedAt: a.CreatedAt, ExpiresAt: a.CreatedAt.AddDate(1, 0, 0)})

	if code, _, errOut := runCLI(t, "corta\n", "admin", "reset-password", "--email", "ana@ejemplo.es"); code != 1 || !strings.Contains(errOut, "12 caracteres") {
		t.Fatalf("contraseña corta: %d %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "otra-clave-bien-larga-2\n", "admin", "reset-password", "--email", "nadie@ejemplo.es"); code != 1 || !strings.Contains(errOut, "no existe") {
		t.Fatalf("correo inexistente: %d %s", code, errOut)
	}
	if code, out, errOut := runCLI(t, "otra-clave-bien-larga-2\n", "admin", "reset-password", "--email", "ana@ejemplo.es"); code != 0 || !strings.Contains(out, "ana@ejemplo.es") {
		t.Fatalf("reset-password: %d %s %s", code, out, errOut)
	}
	a, _ = s.AdminByEmail(ctx, "ana@ejemplo.es")
	if !auth.VerifyPassword(a.PasswordHash, "otra-clave-bien-larga-2") || auth.VerifyPassword(a.PasswordHash, "correcta-y-larga-1") {
		t.Error("la contraseña no cambió")
	}
	if _, _, err := s.SessionByTokenHash(ctx, "h", a.CreatedAt); err != store.ErrNotFound {
		t.Errorf("las sesiones no se cerraron: %v", err)
	}
}

func TestCLI_MissingDatabaseURL(t *testing.T) {
	t.Setenv("MDM_DATABASE_URL", "")
	if code, _, errOut := runCLI(t, "correcta-y-larga-1\n", "admin", "create", "--email", "ana@ejemplo.es"); code != 1 || !strings.Contains(errOut, "MDM_DATABASE_URL") {
		t.Errorf("sin base de datos: %d %s", code, errOut)
	}
}
