package settings

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

func TestLoad_Defaults(t *testing.T) {
	v, err := Load(ctx, storetest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	if !v.AgentAutoUpdate || v.MaxOfflineDays != 14 || v.MinAgentVersion != "" || v.MinOSVersion["windows"] != "" {
		t.Fatalf("valores por defecto: %+v", v)
	}
}

func TestSave_RoundTripAndValidation(t *testing.T) {
	s := storetest.New(t)
	v := Defaults()
	v.MaxOfflineDays = 0
	if err := Save(ctx, s, v, uuid.New(), time.Now()); err == nil {
		t.Error("0 días debería rechazarse")
	}
	v.MaxOfflineDays = 400
	if err := Save(ctx, s, v, uuid.New(), time.Now()); err == nil {
		t.Error("400 días debería rechazarse")
	}
	v.MaxOfflineDays = 30
	v.MinAgentVersion = "abc"
	if err := Save(ctx, s, v, uuid.New(), time.Now()); err == nil {
		t.Error("versión inválida debería rechazarse")
	}
	v.MinAgentVersion = "1.2.0"
	v.MinOSVersion = map[string]string{"windows": "10.0.19045", "linux": "", "solaris": "1"}
	if err := Save(ctx, s, v, uuid.New(), time.Now()); err == nil {
		t.Error("plataforma desconocida debería rechazarse")
	}
	v.MinOSVersion = map[string]string{"windows": "10.0.19045"}
	v.AgentAutoUpdate = false
	if err := Save(ctx, s, v, uuid.New(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(ctx, s)
	if got.AgentAutoUpdate || got.MaxOfflineDays != 30 || got.MinAgentVersion != "1.2.0" || got.MinOSVersion["windows"] != "10.0.19045" || got.MinOSVersion["linux"] != "" {
		t.Fatalf("ida y vuelta: %+v", got)
	}
}

func TestValidVersion(t *testing.T) {
	for _, ok := range []string{"", "1", "1.2.0", "10.0.26100", "1.0.0-rc1", "5.2.21-2ubuntu4"} {
		if !ValidVersion(ok) {
			t.Errorf("%q debería ser válida", ok)
		}
	}
	for _, bad := range []string{"abc", "v1.2", "1..2", "1.2.", " 1.2"} {
		if ValidVersion(bad) {
			t.Errorf("%q no debería ser válida", bad)
		}
	}
}
