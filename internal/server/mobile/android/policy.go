package android

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mdm/internal/protocol"
)

// Policy es una política de la Android Management API (JSON libre).
type Policy map[string]any

func minutesOfDay(hhmm string) int {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return h*60 + m
}

func disabled(params map[string]any) bool {
	b, _ := params["disabled"].(bool)
	return b
}

// Translate convierte los ajustes Android de la política efectiva y los
// despliegues de Google Play en una política de la API. open descifra los
// parámetros secretos (la contraseña de la Wi-Fi).
func Translate(settings []protocol.PolicySetting, apps []protocol.AppAssignment, open func(string) (string, error)) (Policy, error) {
	p := Policy{
		// Sin esto, el dispositivo no informa de su inventario.
		"statusReportingSettings": map[string]any{
			"applicationReportsEnabled": true, "deviceSettingsEnabled": true, "softwareInfoEnabled": true,
			"memoryInfoEnabled": true, "networkInfoEnabled": true, "hardwareStatusEnabled": true,
			"applicationReportingSettings": map[string]any{"includeRemovedApps": false},
		},
	}
	adv := map[string]any{}
	applications := map[string]string{}
	for _, s := range settings {
		var params map[string]any
		_ = json.Unmarshal(s.Params, &params)
		switch s.Key {
		case "android.password":
			p["passwordPolicies"] = []any{map[string]any{"passwordQuality": strings.ToUpper(fmt.Sprint(params["quality"])),
				"passwordMinimumLength": params["min_length"], "passwordScope": "SCOPE_DEVICE"}}
		case "android.screen_lock":
			min, _ := params["minutes"].(float64)
			p["maximumTimeToLock"] = fmt.Sprint(int64(min) * 60000)
		case "android.camera":
			p["cameraAccess"] = map[bool]string{true: "CAMERA_ACCESS_DISABLED", false: "CAMERA_ACCESS_USER_CHOICE"}[disabled(params)]
		case "android.screen_capture":
			p["screenCaptureDisabled"] = disabled(params)
		case "android.usb_file_transfer":
			p["usbDataAccess"] = map[bool]string{true: "DISALLOW_USB_FILE_TRANSFER", false: "ALLOW_USB_DATA_TRANSFER"}[disabled(params)]
		case "android.factory_reset":
			p["factoryResetDisabled"] = disabled(params)
		case "android.developer_options":
			adv["developerSettings"] = map[bool]string{true: "DEVELOPER_SETTINGS_DISABLED", false: "DEVELOPER_SETTINGS_ALLOWED"}[disabled(params)]
		case "android.unknown_sources":
			adv["untrustedAppsPolicy"] = map[bool]string{true: "DISALLOW_INSTALL", false: "ALLOW_INSTALL_DEVICE_WIDE"}[disabled(params)]
		case "android.wifi":
			pass := fmt.Sprint(params["passphrase"])
			if open != nil {
				clear, err := open(pass)
				if err != nil {
					return nil, fmt.Errorf("no se puede descifrar la contraseña de la Wi-Fi: %w", err)
				}
				pass = clear
			}
			ssid := fmt.Sprint(params["ssid"])
			p["openNetworkConfiguration"] = map[string]any{"NetworkConfigurations": []any{map[string]any{
				"GUID": "mdm-" + ssid, "Name": ssid, "Type": "WiFi",
				"WiFi": map[string]any{"SSID": ssid, "Security": "WPA-PSK", "Passphrase": pass, "AutoConnect": true},
			}}}
		case "android.system_updates":
			su := map[string]any{"type": strings.ToUpper(fmt.Sprint(params["type"]))}
			if params["type"] == "windowed" {
				su["startMinutes"] = minutesOfDay(fmt.Sprint(params["window_start"]))
				su["endMinutes"] = minutesOfDay(fmt.Sprint(params["window_end"]))
			}
			p["systemUpdate"] = su
		case "android.kiosk":
			applications[fmt.Sprint(params["package"])] = "KIOSK"
		}
	}
	if len(adv) > 0 {
		p["advancedSecurityOverrides"] = adv
	}
	intent := map[string]string{"required": "FORCE_INSTALLED", "uninstall": "BLOCKED", "available": "AVAILABLE"}
	for _, a := range apps {
		if a.Source != "google_play" {
			continue
		}
		if _, kiosk := applications[a.PackageID]; kiosk {
			continue // la de quiosco ya se instala como tal
		}
		applications[a.PackageID] = intent[a.Intent]
	}
	if len(applications) > 0 {
		names := make([]string, 0, len(applications))
		for n := range applications {
			names = append(names, n)
		}
		sort.Strings(names)
		list := []any{}
		for _, n := range names {
			list = append(list, map[string]any{"packageName": n, "installType": applications[n]})
		}
		p["applications"] = list
	}
	return p, nil
}

// HashPolicy devuelve la huella de una política (para reutilizarla entre
// dispositivos con la misma combinación). json.Marshal ordena las claves de
// los mapas, así que el resultado es estable.
func HashPolicy(p Policy) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// PolicyName es el nombre con que se publica la política en la empresa.
func PolicyName(enterprise string, p Policy) string {
	return enterprise + "/policies/p-" + HashPolicy(p)[:16]
}

// SettingKey traduce un campo de nonComplianceDetails a la clave del catálogo.
func SettingKey(settingName, fieldPath string) string {
	switch settingName {
	case "passwordPolicies", "passwordRequirements":
		return "android.password"
	case "maximumTimeToLock":
		return "android.screen_lock"
	case "cameraAccess", "cameraDisabled":
		return "android.camera"
	case "screenCaptureDisabled":
		return "android.screen_capture"
	case "usbDataAccess", "usbFileTransferDisabled":
		return "android.usb_file_transfer"
	case "factoryResetDisabled":
		return "android.factory_reset"
	case "openNetworkConfiguration":
		return "android.wifi"
	case "systemUpdate":
		return "android.system_updates"
	case "advancedSecurityOverrides":
		if strings.Contains(fieldPath, "developerSettings") {
			return "android.developer_options"
		}
		if strings.Contains(fieldPath, "untrustedAppsPolicy") {
			return "android.unknown_sources"
		}
	}
	return ""
}
