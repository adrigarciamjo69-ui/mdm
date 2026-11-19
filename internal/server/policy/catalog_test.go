package policy

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"mdm/internal/protocol"
)

func TestCatalog_AllSpecKeysPresent(t *testing.T) {
	for platform, keys := range protocol.SettingKeys {
		want := append([]string{protocol.CustomScriptKey}, keys...)
		var got []string
		for _, d := range Catalog {
			if d.Platform == platform {
				got = append(got, d.Key)
				if d.Title == "" || d.Description == "" {
					t.Errorf("%s sin título o descripción", d.Key)
				}
			}
		}
		sort.Strings(want)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s:\n catálogo %v\n esperado %v", platform, got, want)
		}
	}
	audit := map[string]bool{}
	for _, d := range Catalog {
		if d.AuditOnly {
			audit[d.Key] = true
		}
	}
	if len(audit) != 2 || !audit["linux.disk_encryption"] || !audit["linux.forbidden_packages"] {
		t.Errorf("solo auditables: %v", audit)
	}
}

func st(key, mode, params string) Setting {
	return Setting{Key: key, Mode: mode, Params: json.RawMessage(params)}
}

func TestValidateSetting(t *testing.T) {
	ok := []struct {
		platform string
		s        Setting
	}{
		{"windows", st("windows.screen_lock", "enforce", `{"minutes":10}`)},
		{"windows", st("windows.firewall", "audit", `{}`)},
		{"windows", st("windows.firewall", "enforce", ``)},
		{"windows", st("windows.password_policy", "enforce", `{"min_length":12}`)},
		{"linux", st("linux.ssh", "enforce", `{"root_login":false,"password_auth":true}`)},
		{"linux", st("linux.forbidden_packages", "audit", `{"names":["telnetd","nis"]}`)},
		{"linux", st("custom.script", "enforce", `{"interpreter":"bash","detect":"exit 0","remediate":"true","timeout_s":60}`)},
		{"windows", st("custom.script", "audit", `{"interpreter":"powershell","detect":"exit 0"}`)},
	}
	for _, c := range ok {
		if err := ValidateSetting(c.platform, c.s); err != nil {
			t.Errorf("%s %s: %v", c.platform, c.s.Key, err)
		}
	}
	bad := []struct {
		platform string
		s        Setting
	}{
		{"windows", st("windows.screen_lock", "enforce", `{"minutes":0}`)},
		{"windows", st("windows.screen_lock", "enforce", `{"minutes":61}`)},
		{"windows", st("windows.screen_lock", "enforce", `{}`)},
		{"windows", st("windows.screen_lock", "enforce", `{"minutes":"diez"}`)},
		{"windows", st("windows.screen_lock", "enforce", `{"minutes":5,"extra":1}`)},
		{"windows", st("windows.screen_lock", "forzar", `{"minutes":5}`)},
		{"linux", st("linux.disk_encryption", "enforce", `{}`)},
		{"linux", st("windows.firewall", "audit", `{}`)},
		{"linux", st("linux.inventado", "audit", `{}`)},
		{"linux", st("linux.forbidden_packages", "audit", `{"names":[]}`)},
		{"linux", st("custom.script", "audit", `{"interpreter":"powershell","detect":"x"}`)},
		{"linux", st("custom.script", "audit", `{"interpreter":"bash","detect":""}`)},
		{"linux", st("custom.script", "audit", `{"interpreter":"bash","detect":"x","timeout_s":3601}`)},
	}
	for i, c := range bad {
		err := ValidateSetting(c.platform, c.s)
		if err == nil {
			t.Errorf("caso %d (%s) debería fallar", i, c.s.Key)
		} else if !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("caso %d: el mensaje debe ser una frase: %v", i, err)
		}
	}
}

func TestNormalizeAppliesDefaults(t *testing.T) {
	s, err := Normalize("windows", st("windows.bitlocker", "enforce", ``))
	if err != nil || string(s.Params) != `{"escrow_key":true}` {
		t.Fatalf("valores por defecto: %s %v", s.Params, err)
	}
	s, _ = Normalize("linux", st("custom.script", "audit", `{"interpreter":"bash","detect":"exit 0"}`))
	if !strings.Contains(string(s.Params), `"timeout_s":300`) {
		t.Fatalf("timeout por defecto: %s", s.Params)
	}
}
