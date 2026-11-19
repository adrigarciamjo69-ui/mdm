package policy

import (
	"strings"
	"testing"
)

func TestCatalog_AndroidKeys(t *testing.T) {
	want := []string{"android.password", "android.screen_lock", "android.camera", "android.screen_capture", "android.usb_file_transfer",
		"android.factory_reset", "android.developer_options", "android.unknown_sources", "android.wifi", "android.system_updates", "android.kiosk"}
	got := map[string]bool{}
	for _, d := range ForPlatform("android") {
		got[d.Key] = true
		if !d.EnforceOnly {
			t.Errorf("%s: en Android todo se aplica (Android lo hace cumplir él mismo)", d.Key)
		}
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("falta %s", k)
		}
	}
	if len(got) != len(want) {
		t.Errorf("sobran ajustes: %v", got)
	}
}

func TestValidate_Android(t *testing.T) {
	ok := []Setting{
		st("android.password", "enforce", `{"quality":"numeric","min_length":6}`),
		st("android.camera", "enforce", `{"disabled":true}`),
		st("android.wifi", "enforce", `{"ssid":"Oficina","passphrase":"secreta123"}`),
		st("android.system_updates", "enforce", `{"type":"windowed","window_start":"02:00","window_end":"04:00"}`),
		st("android.kiosk", "enforce", `{"package":"com.empresa.almacen"}`),
	}
	for _, s := range ok {
		if err := ValidateSetting("android", s); err != nil {
			t.Errorf("%s: %v", s.Key, err)
		}
	}
	bad := []Setting{
		st("android.camera", "audit", `{"disabled":true}`),
		st("android.password", "enforce", `{"quality":"facil","min_length":6}`),
		st("android.password", "enforce", `{"quality":"numeric","min_length":3}`),
		st("android.system_updates", "enforce", `{"type":"windowed","window_start":"25:00","window_end":"04:00"}`),
		st("android.system_updates", "enforce", `{"type":"windowed"}`),
		st("android.kiosk", "enforce", `{"package":"no es un paquete"}`),
		st("android.wifi", "enforce", `{"ssid":"","passphrase":"x"}`),
	}
	for i, s := range bad {
		if err := ValidateSetting("android", s); err == nil || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("caso %d (%s): %v", i, s.Key, err)
		}
	}
}
