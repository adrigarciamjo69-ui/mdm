// Package batches construye las órdenes de un lote (consola y automatizaciones).
package batches

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

const maxParamValueBytes = 4096

// Skipped es un equipo que no recibe la orden y por qué.
type Skipped struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Reason   string `json:"reason"`
}

// FilterPlatform separa los equipos que pueden ejecutar la orden según su plataforma.
func FilterPlatform(typ string, targets []store.DeviceRow) ([]store.DeviceRow, []Skipped) {
	var keep []store.DeviceRow
	var skipped []Skipped
	for _, t := range targets {
		android := t.Platform == "android"
		switch {
		case android && !protocol.AndroidCommands[typ]:
			skipped = append(skipped, Skipped{ID: t.ID.String(), Hostname: t.Hostname, Reason: "Esta orden no existe en los móviles Android."})
		case !android && protocol.AndroidCommands[typ] && typ != protocol.CmdReboot:
			skipped = append(skipped, Skipped{ID: t.ID.String(), Hostname: t.Hostname, Reason: "Esta orden es solo para móviles Android."})
		default:
			keep = append(keep, t)
		}
	}
	return keep, skipped
}

// Build valida los parámetros de una orden y devuelve la carga y los equipos que pueden recibirla.
func Build(ctx context.Context, st *store.Store, box *secrets.Box, typ string, params json.RawMessage, targets []store.DeviceRow) (json.RawMessage, []store.DeviceRow, []Skipped, error) {
	empty := len(bytes.TrimSpace(params)) == 0 || string(bytes.TrimSpace(params)) == "null" || string(bytes.TrimSpace(params)) == "{}"
	switch typ {
	case protocol.CmdReboot, protocol.CmdShutdown:
		var p protocol.PowerPayload
		if !empty {
			dec := json.NewDecoder(bytes.NewReader(params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return nil, nil, nil, errors.New("Los parámetros deben ser delay_s y message.")
			}
		}
		if p.DelayS < 0 || p.DelayS > 3600 {
			return nil, nil, nil, errors.New("El retraso debe estar entre 0 y 3.600 segundos.")
		}
		p.Message = store.CleanString(p.Message)
		if utf8.RuneCountInString(p.Message) > 200 {
			return nil, nil, nil, errors.New("El mensaje no puede superar los 200 caracteres.")
		}
		b, _ := json.Marshal(p)
		return b, targets, nil, nil
	case protocol.CmdRunScript:
		return scriptPayload(ctx, st, params, targets)
	case protocol.CmdLock, protocol.CmdLostModeStop:
		if !empty {
			return nil, nil, nil, errors.New("Esta orden no admite parámetros.")
		}
		return json.RawMessage(`{}`), targets, nil, nil
	case protocol.CmdResetPassword:
		var p struct {
			NewPassword string `json:"new_password"`
		}
		if err := json.Unmarshal(params, &p); err != nil || utf8.RuneCountInString(p.NewPassword) < 4 || utf8.RuneCountInString(p.NewPassword) > 16 {
			return nil, nil, nil, errors.New("La contraseña nueva debe tener entre 4 y 16 caracteres.")
		}
		// Se guarda cifrada: la tabla de órdenes se conserva 30 días.
		if box == nil {
			return nil, nil, nil, errors.New("El servidor no tiene configurada la clave de cifrado.")
		}
		sealed, err := box.Seal([]byte(p.NewPassword))
		if err != nil {
			return nil, nil, nil, err
		}
		p.NewPassword = sealed
		b, _ := json.Marshal(p)
		return b, targets, nil, nil
	case protocol.CmdLostModeStart:
		var p struct {
			Message string `json:"message"`
			Phone   string `json:"phone"`
		}
		if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Message) == "" || utf8.RuneCountInString(p.Message) > 200 || len(p.Phone) > 30 {
			return nil, nil, nil, errors.New("El modo perdido necesita un mensaje (como mucho 200 caracteres) y, opcionalmente, un teléfono.")
		}
		p.Message, p.Phone = store.CleanString(p.Message), store.CleanString(p.Phone)
		b, _ := json.Marshal(p)
		return b, targets, nil, nil
	case protocol.CmdWipe:
		var p struct {
			WipeExternalStorage bool `json:"wipe_external_storage"`
		}
		if !empty {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, nil, nil, errors.New("El único parámetro es wipe_external_storage.")
			}
		}
		b, _ := json.Marshal(p)
		return b, targets, nil, nil
	case protocol.CmdRefreshInventory, protocol.CmdEvaluatePolicies, protocol.CmdRetire, protocol.CmdScanUpdates, protocol.CmdInstallUpdatesNow:
		if !empty {
			return nil, nil, nil, errors.New("Esta orden no admite parámetros.")
		}
		return json.RawMessage(`{}`), targets, nil, nil
	}
	return nil, nil, nil, errors.New("Tipo de orden no admitido.")
}

func scriptPayload(ctx context.Context, st *store.Store, params json.RawMessage, targets []store.DeviceRow) (json.RawMessage, []store.DeviceRow, []Skipped, error) {
	var in struct {
		ScriptID string            `json:"script_id"`
		Version  int               `json:"version"`
		Params   map[string]string `json:"params"`
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, nil, nil, errors.New("Indica el script (script_id), la versión y sus parámetros.")
	}
	id, err := uuid.Parse(in.ScriptID)
	if err != nil {
		return nil, nil, nil, errors.New("El script indicado no es válido.")
	}
	s, err := st.GetScript(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil, errors.New("El script indicado no existe.")
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if s.ArchivedAt != nil {
		return nil, nil, nil, errors.New("El script está archivado: no se puede ejecutar.")
	}
	v, err := st.GetScriptVersion(ctx, id, in.Version)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil, errors.New("Esa versión del script no existe.")
	}
	if err != nil {
		return nil, nil, nil, err
	}
	env := map[string]string{}
	known := map[string]bool{}
	for _, p := range s.Parameters {
		known[p.Name] = true
		val, given := in.Params[p.Name]
		if !given {
			val = p.Default
		}
		val = store.CleanText(val, maxParamValueBytes)
		if p.Required && strings.TrimSpace(val) == "" {
			return nil, nil, nil, fmt.Errorf("Falta el parámetro obligatorio «%s».", p.Name)
		}
		env["MDM_PARAM_"+p.Name] = val
	}
	for name := range in.Params {
		if !known[name] {
			return nil, nil, nil, fmt.Errorf("El script no tiene el parámetro «%s».", name)
		}
	}
	payload, _ := json.Marshal(protocol.ScriptPayload{Interpreter: s.Interpreter, Content: v.Content, TimeoutS: s.TimeoutS,
		Env: env, ScriptName: s.Name, Version: v.Version})
	var keep []store.DeviceRow
	var skipped []Skipped
	for _, t := range targets {
		if t.Platform != s.Platform {
			skipped = append(skipped, Skipped{ID: t.ID.String(), Hostname: t.Hostname, Reason: "El script es para otra plataforma."})
			continue
		}
		keep = append(keep, t)
	}
	return payload, keep, skipped, nil
}
