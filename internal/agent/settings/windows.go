package settings

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

// Los ajustes de Windows son scripts de PowerShell incluidos en el agente.
// Este fichero no lleva etiqueta de compilación: la lógica se prueba en
// cualquier sistema con un ejecutor falso.

//go:embed scripts/*.ps1
var windowsScripts embed.FS

//go:embed ps_header.ps1
var psHeader string

//go:embed ps_footer.ps1
var psFooter string

const windowsSettingTimeout = 4 * time.Minute

func init() {
	for _, key := range protocol.SettingKeys[protocol.PlatformWindows] {
		key := key
		register(protocol.PlatformWindows, key, func(params json.RawMessage, d Deps) (Setting, error) {
			body, err := windowsScripts.ReadFile("scripts/" + key + ".ps1")
			if err != nil {
				return nil, fmt.Errorf("falta el script del ajuste %s", key)
			}
			env, err := paramsEnv(params)
			if err != nil {
				return nil, err
			}
			escrow := true
			if v, ok := env["MDM_PARAM_ESCROW_KEY"]; ok {
				escrow = v == "true"
			}
			return &winSetting{key: key, script: psHeader + string(body) + "\n" + psFooter, env: env, d: d, escrow: escrow}, nil
		})
	}
}

// paramsEnv convierte los parámetros en variables MDM_PARAM_<NOMBRE>.
func paramsEnv(params json.RawMessage) (map[string]string, error) {
	var m map[string]any
	if err := json.Unmarshal(params, &m); err != nil {
		return nil, fmt.Errorf("parámetros ilegibles: %w", err)
	}
	env := map[string]string{}
	for k, v := range m {
		var s string
		switch x := v.(type) {
		case bool:
			s = fmt.Sprint(x)
		case float64:
			s = fmt.Sprint(int64(x))
		case string:
			s = x
		default:
			b, _ := json.Marshal(x)
			s = string(b)
		}
		env["MDM_PARAM_"+strings.ToUpper(k)] = s
	}
	return env, nil
}

type windowsResult struct {
	Compliant     bool                  `json:"compliant"`
	Detail        string                `json:"detail"`
	NotApplicable bool                  `json:"not_applicable"`
	Recovery      *protocol.RecoveryKey `json:"recovery"`
	Error         string                `json:"error"`
}

// parseWindowsResult toma la última línea JSON de la salida: PowerShell
// puede escribir avisos antes.
func parseWindowsResult(out string) (windowsResult, error) {
	var r windowsResult
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "{") {
			if err := json.Unmarshal([]byte(l), &r); err != nil {
				return r, fmt.Errorf("resultado ilegible del script: %w", err)
			}
			return r, nil
		}
	}
	if strings.TrimSpace(out) == "" {
		return r, errors.New("el script terminó sin resultado")
	}
	return r, fmt.Errorf("el script no devolvió un resultado: %s", lastLine(out))
}

type winSetting struct {
	key      string
	script   string
	env      map[string]string
	d        Deps
	escrow   bool
	recovery *protocol.RecoveryKey
}

func (w *winSetting) run(ctx context.Context, mode string) (windowsResult, error) {
	env := map[string]string{"MDM_MODE": mode}
	for k, v := range w.env {
		env[k] = v
	}
	res := w.d.Exec(ctx, exec.Spec{Interpreter: "powershell", Content: w.script, Env: env, Timeout: windowsSettingTimeout, ScriptDir: w.d.ScriptDir})
	if res.Err != nil {
		return windowsResult{}, res.Err
	}
	if res.TimedOut {
		return windowsResult{}, errors.New("el script del ajuste superó el tiempo máximo")
	}
	r, err := parseWindowsResult(res.Stdout)
	if err != nil {
		if s := lastLine(res.Stderr); s != "" {
			err = fmt.Errorf("%w (%s)", err, s)
		}
		return r, err
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	if res.ExitCode != 0 {
		return r, fmt.Errorf("el script terminó con código %d", res.ExitCode)
	}
	return r, nil
}

func (w *winSetting) Check(ctx context.Context) (bool, string, error) {
	r, err := w.run(ctx, "Test")
	if err != nil {
		return false, "", err
	}
	if r.NotApplicable {
		return false, r.Detail, ErrNotApplicable
	}
	w.recovery = nil
	if w.escrow && r.Recovery != nil && r.Recovery.KeyID != "" && !slices.Contains(w.d.Escrowed, r.Recovery.KeyID) {
		w.recovery = r.Recovery
	}
	return r.Compliant, r.Detail, nil
}

func (w *winSetting) Apply(ctx context.Context) error {
	_, err := w.run(ctx, "Set")
	return err
}

// Recovery devuelve la clave de recuperación pendiente de custodiar, si la hay.
func (w *winSetting) Recovery() *protocol.RecoveryKey { return w.recovery }
