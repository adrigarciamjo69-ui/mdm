package alerts

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/store"
)

func TestGlobalRule_LicenseExpiring(t *testing.T) {
	e := setup(t)
	e.rule(TypeLicenseExpiring, `{"days":30}`)
	exp := e.now.AddDate(0, 0, 10)
	l, err := e.s.CreateLicense(ctx, store.LicenseInput{Name: "Antivirus", LicenseType: "suscripcion", Status: "activa", ExpiryDate: &exp})
	if err != nil {
		t.Fatal(err)
	}
	far := e.now.AddDate(1, 0, 0)
	if _, err := e.s.CreateLicense(ctx, store.LicenseInput{Name: "Lejana", LicenseType: "suscripcion", Status: "activa", ExpiryDate: &far}); err != nil {
		t.Fatal(err)
	}
	e.e.EvaluateState(ctx)
	got := e.open()
	if len(got) != 1 || got[0].DeviceID != nil || !strings.Contains(got[0].Title, "1 licencia") || !strings.Contains(string(got[0].Details), "Antivirus") {
		t.Fatalf("alertas: %+v", got)
	}
	e.e.EvaluateState(ctx)
	if len(e.open()) != 1 {
		t.Fatal("se ha duplicado")
	}
	later := e.now.AddDate(2, 0, 0)
	if _, err := e.s.UpdateLicense(ctx, l.ID, store.LicenseInput{Name: "Antivirus", LicenseType: "suscripcion", Status: "activa", ExpiryDate: &later}); err != nil {
		t.Fatal(err)
	}
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("no se ha resuelto al renovar")
	}
}

func TestGlobalRule_DirectorySyncFailed(t *testing.T) {
	e := setup(t)
	e.rule(TypeDirectorySyncFailed, `{}`)
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("sin configurar no hay alerta")
	}
	_ = e.s.SaveEntraConfig(ctx, store.EntraConfig{TenantID: "t", ClientID: "c", SealedSecret: "s"})
	_ = e.s.RecordDirectorySync(ctx, "Microsoft ha rechazado las credenciales", 0, e.now)
	e.e.EvaluateState(ctx)
	got := e.open()
	if len(got) != 1 || !strings.Contains(got[0].Title, "Entra ID") || !strings.Contains(string(got[0].Details), "rechazado") {
		t.Fatalf("alertas: %+v", got)
	}
	_ = e.s.RecordDirectorySync(ctx, "", 10, e.now)
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("no se ha resuelto")
	}
}

func TestGlobalRule_SecretExpiring(t *testing.T) {
	e := setup(t)
	e.rule(TypeDirectorySecretExpiring, `{}`)
	soon := e.now.AddDate(0, 0, 13)
	_ = e.s.SaveEntraConfig(ctx, store.EntraConfig{TenantID: "t", ClientID: "c", SealedSecret: "s", SecretExpires: &soon})
	e.e.EvaluateState(ctx)
	if got := e.open(); len(got) != 1 || !strings.Contains(got[0].Title, "secreto") {
		t.Fatalf("alertas: %+v", got)
	}
	later := e.now.Add(60 * 24 * time.Hour)
	_ = e.s.SaveEntraConfig(ctx, store.EntraConfig{TenantID: "t", ClientID: "c", SealedSecret: "s", SecretExpires: &later})
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("no se ha resuelto")
	}
}

func TestValidate_NewTypes(t *testing.T) {
	if _, err := Validate(TypeLicenseExpiring, []byte(`{"days":181}`)); err == nil {
		t.Fatal("181 días")
	}
	if _, err := Validate(TypeLicenseExpiring, []byte(`{"days":30}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(TypeDirectorySyncFailed, nil); err != nil {
		t.Fatal(err)
	}
}
