package android

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"mdm/internal/server/mobile/android/fake"
)

var ctx = context.Background()

// testKeyJSON genera una clave de cuenta de servicio con la forma real de Google.
func testKeyJSON(t *testing.T, tokenURI string) []byte {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	b, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "mdm-empresa", "private_key_id": "abc",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "mdm@mdm-empresa.iam.gserviceaccount.com", "token_uri": tokenURI,
	})
	return b
}

func newClient(t *testing.T) (*Client, *fake.Server) {
	f := fake.New()
	t.Cleanup(f.Close)
	sa, err := ParseServiceAccount(testKeyJSON(t, f.TokenURL()))
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(sa, f.APIBase(), nil), f
}

func TestParseServiceAccount(t *testing.T) {
	good := testKeyJSON(t, "https://oauth2.googleapis.com/token")
	sa, err := ParseServiceAccount(good)
	if err != nil || sa.ProjectID != "mdm-empresa" || sa.ClientEmail == "" {
		t.Fatalf("%+v %v", sa, err)
	}
	for name, bad := range map[string]string{
		"no es JSON":      "hola",
		"otro tipo":       `{"type":"authorized_user"}`,
		"sin clave":       `{"type":"service_account","project_id":"p","client_email":"e"}`,
		"clave no válida": `{"type":"service_account","project_id":"p","client_email":"e","private_key":"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"}`,
	} {
		if _, err := ParseServiceAccount([]byte(bad)); err == nil || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestToken_CachedUntilExpiry(t *testing.T) {
	c, f := newClient(t)
	for i := 0; i < 3; i++ {
		if _, err := c.ListDevices(ctx, "enterprises/LC1"); err != nil {
			t.Fatal(err)
		}
	}
	if f.TokenCalls != 1 {
		t.Fatalf("el token debe pedirse una vez y reutilizarse: %d", f.TokenCalls)
	}
	c.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	_, _ = c.ListDevices(ctx, "enterprises/LC1")
	if f.TokenCalls != 2 {
		t.Fatal("caducado, debe pedir otro")
	}
}

func TestSignupAndEnterprise(t *testing.T) {
	c, _ := newClient(t)
	su, err := c.CreateSignupURL(ctx, "https://mdm.ejemplo.es/api/v1/integrations/android/callback")
	if err != nil || !strings.HasPrefix(su.Name, "signupUrls/") || !strings.Contains(su.URL, "callback=") {
		t.Fatalf("%+v %v", su, err)
	}
	if _, err := c.CreateEnterprise(ctx, su.Name, "malo"); err == nil {
		t.Fatal("un enterpriseToken inválido debe fallar")
	}
	e, err := c.CreateEnterprise(ctx, su.Name, fake.GoodEnterpriseToken)
	if err != nil || !strings.HasPrefix(e.Name, "enterprises/") || e.DisplayName == "" {
		t.Fatalf("%+v %v", e, err)
	}
	if err := c.DeleteEnterprise(ctx, e.Name); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentToken(t *testing.T) {
	c, f := newClient(t)
	tok, err := c.CreateEnrollmentToken(ctx, "enterprises/LC1", EnrollmentTokenRequest{PolicyName: "enterprises/LC1/policies/p1",
		Duration: 24 * time.Hour, OneTimeOnly: true, AdditionalData: "token-interno"})
	if err != nil || tok.QRCode == "" || tok.Value == "" || tok.ExpiresAt.IsZero() {
		t.Fatalf("%+v %v", tok, err)
	}
	sent := f.Tokens[tok.Name]
	if sent["duration"] != "86400s" || sent["oneTimeOnly"] != true || sent["additionalData"] != "token-interno" {
		t.Fatalf("petición: %+v", sent)
	}
	if err := c.DeleteEnrollmentToken(ctx, tok.Name); err != nil {
		t.Fatal(err)
	}
}

func TestListDevices_Paginates(t *testing.T) {
	c, f := newClient(t)
	f.PageSize = 2
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		f.AddDevice("enterprises/LC1", id, map[string]any{"state": "ACTIVE", "hardwareInfo": map[string]any{"brand": "samsung", "model": "SM-A556B"}})
	}
	devs, err := c.ListDevices(ctx, "enterprises/LC1")
	if err != nil || len(devs) != 5 || devs[0].HardwareInfo.Model != "SM-A556B" {
		t.Fatalf("%d %v", len(devs), err)
	}
}

func TestPolicyAndDevicePatch(t *testing.T) {
	c, f := newClient(t)
	dev := f.AddDevice("enterprises/LC1", "x", map[string]any{"state": "ACTIVE"})
	if err := c.PatchPolicy(ctx, "enterprises/LC1/policies/p-abc", map[string]any{"cameraAccess": "CAMERA_ACCESS_DISABLED"}); err != nil {
		t.Fatal(err)
	}
	if f.Policy("enterprises/LC1/policies/p-abc")["cameraAccess"] != "CAMERA_ACCESS_DISABLED" {
		t.Fatal("la política no llegó")
	}
	if err := c.SetDevicePolicy(ctx, dev, "enterprises/LC1/policies/p-abc"); err != nil {
		t.Fatal(err)
	}
	if f.Devices[dev]["policyName"] != "enterprises/LC1/policies/p-abc" {
		t.Fatal("el dispositivo no cambió de política")
	}
}

func TestCommandsAndOperations(t *testing.T) {
	c, f := newClient(t)
	dev := f.AddDevice("enterprises/LC1", "x", map[string]any{"state": "ACTIVE"})
	op, err := c.IssueCommand(ctx, dev, Command{Type: "RESET_PASSWORD", NewPassword: "123456", ResetPasswordFlags: []string{"LOCK_NOW"}})
	if err != nil || op.Done || op.Name == "" {
		t.Fatalf("%+v %v", op, err)
	}
	f.CompleteOperation(op.Name, "")
	op, _ = c.GetOperation(ctx, op.Name)
	if !op.Done || op.Error != "" {
		t.Fatalf("%+v", op)
	}
	op2, _ := c.IssueCommand(ctx, dev, Command{Type: "LOCK"})
	f.CompleteOperation(op2.Name, "El dispositivo no admite esta orden.")
	op2, _ = c.GetOperation(ctx, op2.Name)
	if op2.Error != "El dispositivo no admite esta orden." {
		t.Fatalf("%+v", op2)
	}
	if err := c.DeleteDevice(ctx, dev, []string{"WIPE_EXTERNAL_STORAGE"}); err != nil || len(f.Deleted) != 1 || !strings.HasSuffix(f.Deleted[0], "WIPE_EXTERNAL_STORAGE") {
		t.Fatalf("%v %v", err, f.Deleted)
	}
}

func TestErrors_RetryableAndMessage(t *testing.T) {
	c, f := newClient(t)
	f.FailNext("GET /v1/enterprises/LC1/devices", 429, 1)
	_, err := c.ListDevices(ctx, "enterprises/LC1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.Retryable() || apiErr.RetryAfter != time.Second {
		t.Fatalf("429: %v", err)
	}
	f.FailNext("GET /v1/enterprises/LC1/devices", 400, 1)
	_, err = c.ListDevices(ctx, "enterprises/LC1")
	if !errors.As(err, &apiErr) || apiErr.Retryable() || !strings.Contains(err.Error(), "fallo simulado") {
		t.Fatalf("400: %v", err)
	}
	f.FailNext("POST /token", 400, 1)
	c.token = ""
	if _, err := c.ListDevices(ctx, "enterprises/LC1"); err == nil || !strings.Contains(err.Error(), "credenciales de Google") {
		t.Fatalf("token: %v", err)
	}
}
