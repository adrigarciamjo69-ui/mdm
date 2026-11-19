package adminapi

import (
	"testing"
	"time"

	"mdm/internal/server/auth"
)

func TestSummary(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "a", Platform: "windows", OSName: "Windows 11 Pro", OSVersion: "26100", SeenAgo: time.Minute, Inventory: invWith(5, "on")})
	a.seedDevice(seed{Hostname: "b", Platform: "linux", OSName: "Ubuntu", OSVersion: "24.04", SeenAgo: time.Minute, Inventory: invWith(50, "off")})
	a.seedDevice(seed{Hostname: "c", Platform: "windows", OSName: "Windows 11 Pro", OSVersion: "26100", SeenAgo: 8 * 24 * time.Hour, Inventory: invWith(50, "on")})
	a.seedDevice(seed{Hostname: "d", Platform: "windows", OSName: "Windows 10", OSVersion: "19045", SeenAgo: 9 * 24 * time.Hour, Revoked: true, Inventory: invWith(2, "off")})

	type osCount struct {
		Platform  string `json:"platform"`
		OSName    string `json:"os_name"`
		OSVersion string `json:"os_version"`
		Count     int    `json:"count"`
	}
	type summary struct {
		Total             int            `json:"total"`
		ByStatus          map[string]int `json:"by_status"`
		ByPlatform        map[string]int `json:"by_platform"`
		OSVersions        []osCount      `json:"os_versions"`
		LowDisk           int            `json:"low_disk"`
		Unencrypted       int            `json:"unencrypted"`
		Stale             int            `json:"stale"`
		RecentEnrollments []deviceResp   `json:"recent_enrollments"`
	}
	resp := a.loginAs(auth.RoleReadonly).do("GET", "/summary", nil)
	wantStatus(t, resp, 200)
	s := decode[summary](t, resp)
	if s.Total != 4 {
		t.Errorf("total: %d", s.Total)
	}
	if s.ByStatus["online"] != 2 || s.ByStatus["offline"] != 1 || s.ByStatus["revoked"] != 1 || s.ByStatus["inactive"] != 0 {
		t.Errorf("by_status: %v", s.ByStatus)
	}
	if _, ok := s.ByStatus["inactive"]; !ok {
		t.Error("by_status debe incluir todos los estados, también los que valen 0")
	}
	if s.ByPlatform["windows"] != 3 || s.ByPlatform["linux"] != 1 {
		t.Errorf("by_platform: %v", s.ByPlatform)
	}
	// Los equipos revocados no cuentan como problemas pendientes.
	if s.LowDisk != 1 || s.Unencrypted != 1 || s.Stale != 1 {
		t.Errorf("low_disk=%d unencrypted=%d stale=%d", s.LowDisk, s.Unencrypted, s.Stale)
	}
	if len(s.OSVersions) != 3 || s.OSVersions[0].Count != 2 || s.OSVersions[0].OSName != "Windows 11 Pro" {
		t.Errorf("os_versions: %+v", s.OSVersions)
	}
	if len(s.RecentEnrollments) != 4 || s.RecentEnrollments[0].Hostname != "d" || s.RecentEnrollments[3].Hostname != "a" {
		t.Errorf("recent_enrollments: %s", names(pageResp[deviceResp]{Items: s.RecentEnrollments}))
	}
}

func TestSummary_RecentLimitedToTen(t *testing.T) {
	a := newTestAPI(t)
	for i := 0; i < 12; i++ {
		a.seedDevice(seed{})
	}
	type summary struct {
		RecentEnrollments []deviceResp `json:"recent_enrollments"`
	}
	if s := decode[summary](t, a.loginAs(auth.RoleReadonly).do("GET", "/summary", nil)); len(s.RecentEnrollments) != 10 {
		t.Fatalf("recientes: %d", len(s.RecentEnrollments))
	}
}

func TestSummary_Empty(t *testing.T) {
	a := newTestAPI(t)
	body := bodyString(a.loginAs(auth.RoleReadonly).do("GET", "/summary", nil))
	for _, want := range []string{`"total":0`, `"os_versions":[]`, `"recent_enrollments":[]`} {
		if !contains(body, want) {
			t.Errorf("falta %s en %s", want, body)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
