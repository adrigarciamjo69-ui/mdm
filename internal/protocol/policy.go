package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// CustomScriptKey es el ajuste de detección y corrección con scripts propios,
// disponible en todas las plataformas con agente.
const CustomScriptKey = "custom.script"

// SettingKeys son las claves del catálogo de ajustes por plataforma (sin
// custom.script). Es la única lista que comparten servidor y agente: el
// catálogo del servidor y las implementaciones del agente deben cubrir
// exactamente estas claves.
var SettingKeys = map[string][]string{
	PlatformWindows: {
		"windows.bitlocker", "windows.firewall", "windows.defender_realtime", "windows.screen_lock",
		"windows.password_policy", "windows.usb_storage", "windows.smb1", "windows.guest_account",
		"windows.autologon", "windows.remote_desktop", "windows.domain_join", "windows.power",
	},
	PlatformLinux: {
		"linux.firewall", "linux.auto_security_updates", "linux.ssh", "linux.disk_encryption", "linux.forbidden_packages",
	},
}

// Modos de un ajuste.
const (
	ModeAudit   = "audit"
	ModeEnforce = "enforce"
)

// Estados del resultado de un ajuste.
const (
	StatusCompliant     = "compliant"
	StatusNoncompliant  = "noncompliant"
	StatusError         = "error"
	StatusNotApplicable = "not_applicable"
)

// DefaultEvaluateIntervalS es cada cuánto reevalúa el agente su política.
const DefaultEvaluateIntervalS = 900

type PolicySetting struct {
	PolicyID string          `json:"policy_id"`
	Key      string          `json:"key"`
	Mode     string          `json:"mode"`
	Params   json.RawMessage `json:"params"`
}

type EffectivePolicy struct {
	Hash              string          `json:"hash"`
	EvaluateIntervalS int             `json:"evaluate_interval_s"`
	Settings          []PolicySetting `json:"settings"`
	// EscrowedKeyIDs son los protectores de BitLocker ya custodiados en el servidor.
	EscrowedKeyIDs []string `json:"escrowed_key_ids"`
	// Apps son los despliegues de software que aplican al equipo (fase 3).
	Apps []AppAssignment `json:"apps"`
	// Patches es la política de parches del equipo, si tiene alguna (fase 3).
	Patches *PatchPolicy `json:"patches,omitempty"`
}

type SettingResult struct {
	PolicyID string `json:"policy_id"`
	Key      string `json:"key"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Applied  bool   `json:"applied"`
}

type ComplianceReport struct {
	PolicyHash  string          `json:"policy_hash"`
	EvaluatedAt time.Time       `json:"evaluated_at"`
	Results     []SettingResult `json:"results"`
}

type RecoveryKey struct {
	Volume           string `json:"volume"`
	KeyID            string `json:"key_id"`
	RecoveryPassword string `json:"recovery_password"`
}

// PolicyHash devuelve la huella de una política efectiva. No depende del
// orden de los ajustes ni de los ids custodiados, ni del formato del JSON de
// los parámetros.
func PolicyHash(settings []PolicySetting, escrowed []string) string {
	s := make([]PolicySetting, len(settings))
	for i, x := range settings {
		x.Params = compactJSON(x.Params)
		s[i] = x
	}
	sort.Slice(s, func(i, j int) bool {
		if s[i].Key != s[j].Key {
			return s[i].Key < s[j].Key
		}
		return s[i].PolicyID < s[j].PolicyID
	})
	e := append([]string{}, escrowed...)
	sort.Strings(e)
	b, _ := json.Marshal(struct {
		S []PolicySetting `json:"s"`
		E []string        `json:"e"`
	}{s, e})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}
