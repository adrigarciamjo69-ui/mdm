package android

import (
	"encoding/json"
	"strings"
	"testing"

	"mdm/internal/protocol"
)

func ps(key, params string) protocol.PolicySetting {
	return protocol.PolicySetting{PolicyID: "p1", Key: key, Mode: "enforce", Params: json.RawMessage(params)}
}

func open(s string) (string, error) { return strings.TrimPrefix(s, "sellado:"), nil }

func translate(t *testing.T, settings []protocol.PolicySetting, apps []protocol.AppAssignment) map[string]any {
	t.Helper()
	p, err := Translate(settings, apps, open)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func TestTranslate_Settings(t *testing.T) {
	m := translate(t, []protocol.PolicySetting{
		ps("android.password", `{"quality":"alphanumeric","min_length":8}`),
		ps("android.screen_lock", `{"minutes":5}`),
		ps("android.camera", `{"disabled":true}`),
		ps("android.screen_capture", `{"disabled":true}`),
		ps("android.usb_file_transfer", `{"disabled":true}`),
		ps("android.factory_reset", `{"disabled":true}`),
		ps("android.developer_options", `{"disabled":true}`),
		ps("android.unknown_sources", `{"disabled":true}`),
		ps("android.system_updates", `{"type":"windowed","window_start":"02:00","window_end":"04:30"}`),
	}, nil)
	pp := m["passwordPolicies"].([]any)[0].(map[string]any)
	if pp["passwordQuality"] != "ALPHANUMERIC" || pp["passwordMinimumLength"] != float64(8) || pp["passwordScope"] != "SCOPE_DEVICE" {
		t.Errorf("contraseña: %v", pp)
	}
	if m["maximumTimeToLock"] != "300000" {
		t.Errorf("bloqueo: %v", m["maximumTimeToLock"])
	}
	if m["cameraAccess"] != "CAMERA_ACCESS_DISABLED" || m["screenCaptureDisabled"] != true || m["usbDataAccess"] != "DISALLOW_USB_FILE_TRANSFER" ||
		m["factoryResetDisabled"] != true {
		t.Errorf("restricciones: %v", m)
	}
	adv := m["advancedSecurityOverrides"].(map[string]any)
	if adv["developerSettings"] != "DEVELOPER_SETTINGS_DISABLED" || adv["untrustedAppsPolicy"] != "DISALLOW_INSTALL" {
		t.Errorf("avanzadas: %v", adv)
	}
	su := m["systemUpdate"].(map[string]any)
	if su["type"] != "WINDOWED" || su["startMinutes"] != float64(120) || su["endMinutes"] != float64(270) {
		t.Errorf("actualizaciones: %v", su)
	}
	sr := m["statusReportingSettings"].(map[string]any)
	if sr["applicationReportsEnabled"] != true || sr["softwareInfoEnabled"] != true || sr["networkInfoEnabled"] != true {
		t.Errorf("informes de estado: %v", sr)
	}
}

func TestTranslate_DisabledFalseAllows(t *testing.T) {
	m := translate(t, []protocol.PolicySetting{ps("android.camera", `{"disabled":false}`), ps("android.unknown_sources", `{"disabled":false}`)}, nil)
	if m["cameraAccess"] != "CAMERA_ACCESS_USER_CHOICE" || m["advancedSecurityOverrides"].(map[string]any)["untrustedAppsPolicy"] != "ALLOW_INSTALL_DEVICE_WIDE" {
		t.Errorf("%v", m)
	}
}

func TestTranslate_WifiOpensSecret(t *testing.T) {
	m := translate(t, []protocol.PolicySetting{ps("android.wifi", `{"ssid":"Oficina","passphrase":"sellado:secreta123"}`)}, nil)
	onc := m["openNetworkConfiguration"].(map[string]any)
	wifi := onc["NetworkConfigurations"].([]any)[0].(map[string]any)["WiFi"].(map[string]any)
	if wifi["SSID"] != "Oficina" || wifi["Passphrase"] != "secreta123" || wifi["Security"] != "WPA-PSK" {
		t.Errorf("wifi: %v", wifi)
	}
}

func TestTranslate_AppsAndKiosk(t *testing.T) {
	m := translate(t, []protocol.PolicySetting{ps("android.kiosk", `{"package":"com.empresa.almacen"}`)}, []protocol.AppAssignment{
		{DeploymentID: "d1", Source: "google_play", PackageID: "com.microsoft.teams", Intent: "required"},
		{DeploymentID: "d2", Source: "google_play", PackageID: "com.facebook.katana", Intent: "uninstall"},
		{DeploymentID: "d3", Source: "google_play", PackageID: "com.google.android.apps.maps", Intent: "available"},
	})
	got := map[string]string{}
	for _, a := range m["applications"].([]any) {
		app := a.(map[string]any)
		got[app["packageName"].(string)] = app["installType"].(string)
	}
	want := map[string]string{"com.microsoft.teams": "FORCE_INSTALLED", "com.facebook.katana": "BLOCKED",
		"com.google.android.apps.maps": "AVAILABLE", "com.empresa.almacen": "KIOSK"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, se esperaba %s", k, got[k], v)
		}
	}
}

func TestPolicyHash_StableAndDistinct(t *testing.T) {
	a, _ := Translate([]protocol.PolicySetting{ps("android.camera", `{"disabled":true}`), ps("android.screen_lock", `{"minutes":5}`)}, nil, open)
	b, _ := Translate([]protocol.PolicySetting{ps("android.screen_lock", `{"minutes":5}`), ps("android.camera", `{"disabled":true}`)}, nil, open)
	c, _ := Translate([]protocol.PolicySetting{ps("android.camera", `{"disabled":false}`)}, nil, open)
	if HashPolicy(a) != HashPolicy(b) || HashPolicy(a) == HashPolicy(c) {
		t.Fatal("misma combinación, misma huella; distinta, distinta")
	}
	if !strings.HasPrefix(PolicyName("enterprises/LC1", a), "enterprises/LC1/policies/p-") || len(PolicyName("enterprises/LC1", a)) != len("enterprises/LC1/policies/p-")+16 {
		t.Fatalf("nombre: %s", PolicyName("enterprises/LC1", a))
	}
}

func TestComplianceMapping(t *testing.T) {
	cases := map[string]string{"passwordPolicies": "android.password", "maximumTimeToLock": "android.screen_lock", "cameraAccess": "android.camera",
		"systemUpdate": "android.system_updates", "openNetworkConfiguration": "android.wifi", "usbDataAccess": "android.usb_file_transfer"}
	for setting, key := range cases {
		if got := SettingKey(setting, ""); got != key {
			t.Errorf("%s → %s, se esperaba %s", setting, got, key)
		}
	}
	if SettingKey("advancedSecurityOverrides", "advancedSecurityOverrides.developerSettings") != "android.developer_options" ||
		SettingKey("advancedSecurityOverrides", "advancedSecurityOverrides.untrustedAppsPolicy") != "android.unknown_sources" {
		t.Error("ajustes avanzados")
	}
	if SettingKey("loQueSea", "") != "" {
		t.Error("desconocido → vacío")
	}
}
