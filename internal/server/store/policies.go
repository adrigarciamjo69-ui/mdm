package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
	"mdm/internal/server/policy"
)

type PolicyAssignment struct {
	GroupID   *uuid.UUID
	GroupName string
	Exclude   bool
}

type Policy struct {
	ID          uuid.UUID
	Name        string
	Description string
	Platform    string
	Priority    int
	Settings    []policy.Setting
	Assignments []PolicyAssignment
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NewPolicy struct {
	Name, Description, Platform string
	Settings                    []policy.Setting
	CreatedBy                   uuid.UUID
}

const policyCols = `id, name, description, platform, priority, settings, created_at, updated_at`

func scanPolicy(row pgx.Row) (Policy, error) {
	var p Policy
	var raw []byte
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Platform, &p.Priority, &raw, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p.Settings); err != nil {
		return p, fmt.Errorf("ajustes ilegibles de la política %s: %w", p.ID, err)
	}
	// jsonb devuelve el JSON con espacios; se compacta para que la API y el
	// agente reciban siempre la misma forma.
	for i := range p.Settings {
		var buf bytes.Buffer
		if json.Compact(&buf, p.Settings[i].Params) == nil {
			p.Settings[i].Params = buf.Bytes()
		}
	}
	return p, nil
}

func settingsJSON(s []policy.Setting) []byte {
	if s == nil {
		s = []policy.Setting{}
	}
	b, _ := json.Marshal(s)
	return b
}

// CreatePolicy crea una política con la prioridad más baja (el número más alto).
func (s *Store) CreatePolicy(ctx context.Context, n NewPolicy) (Policy, error) {
	p, err := scanPolicy(s.pool.QueryRow(ctx,
		`INSERT INTO policies (name, description, platform, priority, settings, created_by)
		 VALUES ($1, $2, $3, (SELECT COALESCE(max(priority), 0) + 1 FROM policies), $4, $5)
		 RETURNING `+policyCols, n.Name, n.Description, n.Platform, settingsJSON(n.Settings), n.CreatedBy))
	if isUniqueViolation(err) {
		return Policy{}, ErrConflict
	}
	if err != nil {
		return Policy{}, fmt.Errorf("crear política: %w", err)
	}
	p.Assignments = []PolicyAssignment{}
	return p, nil
}

func (s *Store) loadAssignments(ctx context.Context, ps []Policy) error {
	if len(ps) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(ps))
	for i := range ps {
		idx[ps[i].ID] = i
		ids[i] = ps[i].ID
		ps[i].Assignments = []PolicyAssignment{}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.policy_id, a.group_id, COALESCE(g.name, ''), a.exclude
		 FROM policy_assignments a LEFT JOIN groups g ON g.id = a.group_id
		 WHERE a.policy_id = ANY($1) ORDER BY a.exclude, lower(COALESCE(g.name, ''))`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid uuid.UUID
		var a PolicyAssignment
		if err := rows.Scan(&pid, &a.GroupID, &a.GroupName, &a.Exclude); err != nil {
			return err
		}
		ps[idx[pid]].Assignments = append(ps[idx[pid]].Assignments, a)
	}
	return rows.Err()
}

func (s *Store) GetPolicy(ctx context.Context, id uuid.UUID) (Policy, error) {
	p, err := scanPolicy(s.pool.QueryRow(ctx, `SELECT `+policyCols+` FROM policies WHERE id = $1`, id))
	if err != nil {
		return p, err
	}
	ps := []Policy{p}
	err = s.loadAssignments(ctx, ps)
	return ps[0], err
}

// ListPolicies devuelve las políticas por prioridad; platform vacío = todas.
func (s *Store) ListPolicies(ctx context.Context, platform string) ([]Policy, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+policyCols+` FROM policies WHERE $1 = '' OR platform = $1 ORDER BY priority`, platform)
	if err != nil {
		return nil, fmt.Errorf("listar políticas: %w", err)
	}
	out := []Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadAssignments(ctx, out)
}

func (s *Store) UpdatePolicy(ctx context.Context, id uuid.UUID, name, description string, settings []policy.Setting, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE policies SET name = $2, description = $3, settings = $4, updated_at = $5 WHERE id = $1`,
		id, name, description, settingsJSON(settings), at)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("actualizar política: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM policies WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAssignments sustituye las asignaciones de la política.
func (s *Store) SetAssignments(ctx context.Context, id uuid.UUID, as []policy.Assignment, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		tag, err := q.Exec(ctx, `UPDATE policies SET updated_at = $2 WHERE id = $1`, id, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := q.Exec(ctx, `DELETE FROM policy_assignments WHERE policy_id = $1`, id); err != nil {
			return err
		}
		for _, a := range as {
			if _, err := q.Exec(ctx, `INSERT INTO policy_assignments (policy_id, group_id, exclude) VALUES ($1, $2, $3)`, id, a.GroupID, a.Exclude); err != nil {
				if isForeignKeyViolation(err) {
					return ErrBadReference
				}
				return err
			}
		}
		return nil
	})
}

// ReorderPolicies fija las prioridades en el orden dado (1, 2, 3…). La lista
// debe contener exactamente todas las políticas.
func (s *Store) ReorderPolicies(ctx context.Context, ids []uuid.UUID) error {
	return s.Tx(ctx, func(q Querier) error {
		var total int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM policies`).Scan(&total); err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if total != len(ids) || len(seen) != len(ids) {
			return ErrConflict
		}
		for i, id := range ids {
			tag, err := q.Exec(ctx, `UPDATE policies SET priority = $2 WHERE id = $1`, id, i+1)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrConflict
			}
		}
		return nil
	})
}

func toAssigned(p Policy) policy.AssignedPolicy {
	ap := policy.AssignedPolicy{ID: p.ID, Name: p.Name, Priority: p.Priority, Platform: p.Platform, Settings: p.Settings}
	for _, a := range p.Assignments {
		ap.Assignments = append(ap.Assignments, policy.Assignment{GroupID: a.GroupID, Exclude: a.Exclude})
	}
	return ap
}

// AssignedPolicies devuelve las políticas de la plataforma listas para calcular.
func (s *Store) AssignedPolicies(ctx context.Context, platform string) ([]policy.AssignedPolicy, error) {
	ps, err := s.ListPolicies(ctx, platform)
	if err != nil {
		return nil, err
	}
	out := make([]policy.AssignedPolicy, 0, len(ps))
	for _, p := range ps {
		out = append(out, toAssigned(p))
	}
	return out, nil
}

// effectivePatches añade la política de parches del equipo (tarea de parches).
var effectivePatches = func(ctx context.Context, s *Store, deviceID uuid.UUID, platform string, groups []uuid.UUID, eff *protocol.EffectivePolicy) error {
	return nil
}

// escrowedKeyIDs devuelve los protectores de BitLocker custodiados del equipo.
// La tabla llega en la tarea 11; mientras tanto no hay ninguno.
var escrowedKeyIDs = func(ctx context.Context, s *Store, deviceID uuid.UUID) ([]string, error) { return nil, nil }

// EffectivePolicyFor calcula la política efectiva de un equipo.
func (s *Store) EffectivePolicyFor(ctx context.Context, deviceID uuid.UUID) (protocol.EffectivePolicy, []policy.Conflict, error) {
	var platform string
	if err := s.pool.QueryRow(ctx, `SELECT platform FROM devices WHERE id = $1`, deviceID).Scan(&platform); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return protocol.EffectivePolicy{}, nil, ErrNotFound
		}
		return protocol.EffectivePolicy{}, nil, err
	}
	groups, err := s.DeviceGroupIDs(ctx, deviceID)
	if err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	ps, err := s.AssignedPolicies(ctx, platform)
	if err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	esc, err := escrowedKeyIDs(ctx, s, deviceID)
	if err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	eff, conflicts := policy.Effective(ps, groups, platform, esc)
	deps, err := s.AssignedDeployments(ctx, platform)
	if err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	epochs, err := s.retryEpochs(ctx, deviceID)
	if err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	eff.Apps, _ = policy.EffectiveApps(deps, groups, platform, epochs)
	if err := effectivePatches(ctx, s, deviceID, platform, groups, &eff); err != nil {
		return protocol.EffectivePolicy{}, nil, err
	}
	eff.Hash = protocol.PolicyHashFull(eff)
	return eff, conflicts, nil
}

type PolicyDevice struct {
	ID       uuid.UUID
	Hostname string
	Device   Device
}

// PolicyDevices devuelve los equipos no revocados a los que aplica la política.
func (s *Store) PolicyDevices(ctx context.Context, id uuid.UUID) ([]PolicyDevice, error) {
	p, err := s.GetPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.DevicesForAssignments(ctx, p)
}

// DevicesForAssignments devuelve los equipos no revocados de la plataforma
// de p a los que aplican sus asignaciones.
func (s *Store) DevicesForAssignments(ctx context.Context, p Policy) ([]PolicyDevice, error) {
	ap := toAssigned(p)
	rows, err := s.pool.Query(ctx,
		`SELECT `+deviceCols+`,
		        ARRAY(SELECT m.group_id FROM group_members m WHERE m.device_id = d.id)
		 FROM devices d WHERE d.platform = $1 AND d.revoked_at IS NULL ORDER BY lower(d.hostname)`, p.Platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PolicyDevice{}
	for rows.Next() {
		var d Device
		var groups []uuid.UUID
		if err := rows.Scan(append(deviceDest(&d), &groups)...); err != nil {
			return nil, err
		}
		if d.GroupID != nil {
			groups = append(groups, *d.GroupID)
		}
		if policy.Applies(ap, p.Platform, groups) {
			out = append(out, PolicyDevice{ID: d.ID, Hostname: d.Hostname, Device: d})
		}
	}
	return out, rows.Err()
}
