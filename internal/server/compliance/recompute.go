package compliance

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

// Recompute calcula y guarda el estado de cumplimiento de un equipo.
// Devuelve si el estado cambió.
func Recompute(ctx context.Context, s *store.Store, deviceID uuid.UUID, now time.Time) (bool, error) {
	v, err := settings.Load(ctx, s)
	if err != nil {
		return false, err
	}
	return recomputeWith(ctx, s, deviceID, v, now)
}

func recomputeWith(ctx context.Context, s *store.Store, deviceID uuid.UUID, v settings.Values, now time.Time) (bool, error) {
	dev, err := s.DeviceByID(ctx, deviceID)
	if err != nil {
		return false, err
	}
	eff, _, err := s.EffectivePolicyFor(ctx, deviceID)
	if err != nil {
		return false, err
	}
	rec, rows, err := s.Compliance(ctx, deviceID)
	if err != nil {
		return false, err
	}
	var approved []string
	if eff.Patches != nil {
		approved = eff.Patches.ApprovedUpdateIDs
	}
	oldest, err := s.OldestApprovedSecurityUpdate(ctx, deviceID, approved)
	if err != nil {
		return false, err
	}
	var mobile protocol.Mobile
	if dev.Platform == "android" {
		if raw, _, err := s.DeviceInventory(ctx, deviceID); err == nil {
			var inv protocol.Inventory
			if json.Unmarshal(raw, &inv) == nil && inv.Mobile != nil {
				mobile = *inv.Mobile
			}
		}
	}
	results := make([]protocol.SettingResult, len(rows))
	for i, r := range rows {
		results[i] = r.SettingResult
	}
	res := Compute(Input{Effective: eff, ReportHash: rec.ReportHash, ReportedAt: rec.ReportedAt, Results: results,
		Device: DeviceInfo{Platform: dev.Platform, LastSeenAt: dev.LastSeenAt, AgentVersion: dev.AgentVersion, OSVersion: dev.OSVersion,
			OldestSecurityUpdate: oldest, SecurityPatchLevel: mobile.SecurityPatchLevel, Integrity: mobile.Integrity},
		Settings: v, Now: now})
	since := NextSince(rec.State, res.State, rec.NoncompliantSince, now)
	return s.SaveComplianceState(ctx, deviceID, res.State, res.Reasons, since, now)
}

// RecomputeAll recalcula el cumplimiento de todos los equipos y devuelve los
// que cambiaron de estado.
func RecomputeAll(ctx context.Context, s *store.Store, now time.Time) ([]uuid.UUID, error) {
	v, err := settings.Load(ctx, s)
	if err != nil {
		return nil, err
	}
	ids, err := s.DeviceIDs(ctx)
	if err != nil {
		return nil, err
	}
	var changed []uuid.UUID
	for _, id := range ids {
		c, err := recomputeWith(ctx, s, id, v, now)
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			return changed, err
		}
		if c {
			changed = append(changed, id)
		}
	}
	return changed, nil
}
