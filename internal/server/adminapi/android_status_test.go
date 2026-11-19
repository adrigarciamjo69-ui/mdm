package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
)

// Un móvil informa con menos frecuencia: está «en línea» si informó en las
// últimas 24 horas. Go, el filtro SQL y el resumen deben coincidir.
func TestAndroidOnlineThreshold(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "movil-10h", Platform: "android", SeenAgo: 10 * time.Hour})
	a.seedDevice(seed{Hostname: "movil-30h", Platform: "android", SeenAgo: 30 * time.Hour})
	a.seedDevice(seed{Hostname: "pc-10h", Platform: "windows", SeenAgo: 10 * time.Hour})
	ro := a.loginAs(auth.RoleReadonly)
	if got := names(list(t, ro, "status=online")); got != "movil-10h" {
		t.Fatalf("en línea: %q", got)
	}
	if got := names(list(t, ro, "status=offline")); got != "movil-30h,pc-10h" {
		t.Fatalf("desconectados: %q", got)
	}
	d := decode[pageResp[deviceResp]](t, ro.do("GET", "/devices?q=movil-10h", nil))
	if d.Items[0].Status != "online" {
		t.Fatalf("ficha: %+v", d.Items[0])
	}
	type summary struct {
		ByStatus   map[string]int `json:"by_status"`
		ByPlatform map[string]int `json:"by_platform"`
	}
	s := decode[summary](t, ro.do("GET", "/summary", nil))
	if s.ByStatus["online"] != 1 || s.ByStatus["offline"] != 2 || s.ByPlatform["android"] != 2 {
		t.Fatalf("resumen: %+v", s)
	}
	_ = strings.Join
}
