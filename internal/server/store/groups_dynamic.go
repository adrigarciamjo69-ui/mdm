package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
	"mdm/internal/server/rules"
)

// checkinIntervalS es el intervalo por defecto con el que se calcula el
// estado de un equipo en las reglas.
const checkinIntervalS = protocol.DefaultCheckinIntervalS

const factsQuery = `
SELECT d.id, d.platform, COALESCE(d.group_id::text, ''), d.os_name, d.os_version, d.agent_version, d.hostname,
       d.manufacturer, d.model, d.assigned_user, COALESCE(i.data->'system'->>'domain', ''), d.tags,
       d.last_seen_at, d.revoked_at, d.enrolled_at, COALESCE(c.state, 'unknown'),
       ARRAY(SELECT s.name FROM device_software s WHERE s.device_id = d.id)
FROM devices d
LEFT JOIN device_inventory i ON i.device_id = d.id
LEFT JOIN device_compliance c ON c.device_id = d.id`

func scanFacts(row pgx.Row, now time.Time) (uuid.UUID, rules.Facts, error) {
	var id uuid.UUID
	var f rules.Facts
	var lastSeen, revoked *time.Time
	var enrolled time.Time
	err := row.Scan(&id, &f.Platform, &f.Group, &f.OSName, &f.OSVersion, &f.AgentVersion, &f.Hostname,
		&f.Manufacturer, &f.Model, &f.AssignedUser, &f.Domain, &f.Tags, &lastSeen, &revoked, &enrolled, &f.Compliance, &f.Software)
	if err != nil {
		return uuid.Nil, f, err
	}
	f.Status = string(StatusOf(Device{Platform: f.Platform, LastSeenAt: lastSeen, RevokedAt: revoked}, now, checkinIntervalS))
	// Un equipo que nunca ha conectado cuenta desde su inscripción.
	since := enrolled
	if lastSeen != nil {
		since = *lastSeen
	}
	f.LastSeenDays = int(now.Sub(since).Hours() / 24)
	return id, f, nil
}

// DeviceFacts devuelve los datos de un equipo sobre los que se evalúan las reglas.
func (s *Store) DeviceFacts(ctx context.Context, id uuid.UUID, now time.Time) (rules.Facts, error) {
	_, f, err := scanFacts(s.pool.QueryRow(ctx, factsQuery+` WHERE d.id = $1`, id), now)
	if err == pgx.ErrNoRows {
		return f, ErrNotFound
	}
	return f, err
}

// AllDeviceFacts devuelve los datos de todos los equipos.
func (s *Store) AllDeviceFacts(ctx context.Context, now time.Time) (map[uuid.UUID]rules.Facts, error) {
	rows, err := s.pool.Query(ctx, factsQuery)
	if err != nil {
		return nil, fmt.Errorf("leer datos de equipos: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]rules.Facts{}
	for rows.Next() {
		id, f, err := scanFacts(rows, now)
		if err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rows.Err()
}

type dynamicGroup struct {
	ID   uuid.UUID
	Rule rules.Rule
}

func (s *Store) dynamicGroups(ctx context.Context, only *uuid.UUID) ([]dynamicGroup, error) {
	q := `SELECT id, rule FROM groups WHERE kind = 'dynamic'`
	args := []any{}
	if only != nil {
		q += ` AND id = $1`
		args = append(args, *only)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dynamicGroup
	for rows.Next() {
		var g dynamicGroup
		var raw []byte
		if err := rows.Scan(&g.ID, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &g.Rule); err != nil {
			return nil, fmt.Errorf("regla ilegible del grupo %s: %w", g.ID, err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RecomputeDevice recalcula a qué grupos dinámicos pertenece un equipo.
// Devuelve si cambió algo.
func (s *Store) RecomputeDevice(ctx context.Context, deviceID uuid.UUID, now time.Time) (bool, error) {
	groups, err := s.dynamicGroups(ctx, nil)
	if err != nil {
		return false, err
	}
	facts, err := s.DeviceFacts(ctx, deviceID, now)
	if err == ErrNotFound {
		return false, nil // el equipo se borró entre tanto
	}
	if err != nil {
		return false, err
	}
	changed := false
	err = s.Tx(ctx, func(q Querier) error {
		for _, g := range groups {
			var tag interface{ RowsAffected() int64 }
			var err error
			if rules.Matches(g.Rule, facts) {
				tag, err = q.Exec(ctx, `INSERT INTO group_members (group_id, device_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, g.ID, deviceID)
			} else {
				tag, err = q.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND device_id = $2`, g.ID, deviceID)
			}
			if err != nil {
				return err
			}
			changed = changed || tag.RowsAffected() > 0
		}
		return nil
	})
	return changed, err
}

// RecomputeGroup recalcula los miembros de un grupo dinámico.
func (s *Store) RecomputeGroup(ctx context.Context, groupID uuid.UUID, now time.Time) error {
	groups, err := s.dynamicGroups(ctx, &groupID)
	if err != nil || len(groups) == 0 {
		return err
	}
	facts, err := s.AllDeviceFacts(ctx, now)
	if err != nil {
		return err
	}
	return s.replaceMembers(ctx, groups[0], facts)
}

// RecomputeAll recalcula todos los grupos dinámicos.
func (s *Store) RecomputeAll(ctx context.Context, now time.Time) error {
	groups, err := s.dynamicGroups(ctx, nil)
	if err != nil || len(groups) == 0 {
		return err
	}
	facts, err := s.AllDeviceFacts(ctx, now)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err := s.replaceMembers(ctx, g, facts); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) replaceMembers(ctx context.Context, g dynamicGroup, facts map[uuid.UUID]rules.Facts) error {
	members := []uuid.UUID{}
	for id, f := range facts {
		if rules.Matches(g.Rule, f) {
			members = append(members, id)
		}
	}
	return s.Tx(ctx, func(q Querier) error {
		if _, err := q.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND NOT (device_id = ANY($2))`, g.ID, members); err != nil {
			return err
		}
		_, err := q.Exec(ctx,
			`INSERT INTO group_members (group_id, device_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, g.ID, members)
		return err
	})
}

// DeviceGroupIDs devuelve el grupo principal del equipo y sus grupos dinámicos.
func (s *Store) DeviceGroupIDs(ctx context.Context, deviceID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT group_id FROM devices WHERE id = $1 AND group_id IS NOT NULL
		 UNION SELECT group_id FROM group_members WHERE device_id = $1`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type PreviewDevice struct {
	ID       uuid.UUID
	Hostname string
}

// PreviewRule devuelve cuántos equipos cumplen la regla y los 10 primeros por nombre.
func (s *Store) PreviewRule(ctx context.Context, r rules.Rule, now time.Time) (int, []PreviewDevice, error) {
	facts, err := s.AllDeviceFacts(ctx, now)
	if err != nil {
		return 0, nil, err
	}
	var all []PreviewDevice
	for id, f := range facts {
		if rules.Matches(r, f) {
			all = append(all, PreviewDevice{ID: id, Hostname: f.Hostname})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Hostname < all[j].Hostname })
	n := len(all)
	if len(all) > 10 {
		all = all[:10]
	}
	return n, all, nil
}

// EnqueueRecomputeDevice encola el recálculo de los grupos de un equipo.
func EnqueueRecomputeDevice(ctx context.Context, q Querier, deviceID uuid.UUID, now time.Time) error {
	return EnqueueJob(ctx, q, NewJob{Kind: "groups.recompute", Payload: map[string]string{"device_id": deviceID.String()},
		RunAt: now, UniqueKey: "groups.recompute:device:" + deviceID.String()})
}
