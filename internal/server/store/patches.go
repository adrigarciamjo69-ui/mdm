package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
	"mdm/internal/server/patches"
	"mdm/internal/server/policy"
)

type UpdateRow struct {
	ID             string
	KB             string
	Title          string
	Classification string
	Severity       string
	SizeBytes      int64
	PublishedAt    *time.Time
	Security       bool
	State          string
	FirstSeenAt    time.Time
	InstalledAt    *time.Time
	LastError      string
}

type PatchState struct {
	LastScanAt    *time.Time
	LastInstallAt *time.Time
	RebootPending bool
}

// SaveUpdateScan sustituye la lista de actualizaciones pendientes del equipo
// por la de la búsqueda, conservando cuándo se vio cada una por primera vez.
func (s *Store) SaveUpdateScan(ctx context.Context, deviceID uuid.UUID, platform string, scan protocol.UpdateScan, now time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		ids := make([]string, 0, len(scan.Updates))
		for _, u := range scan.Updates {
			ids = append(ids, u.ID)
			if _, err := q.Exec(ctx,
				`INSERT INTO updates (platform, id, kb, title, classification, severity, size_bytes, published_at, security, first_seen_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				 ON CONFLICT (platform, id) DO UPDATE SET kb = EXCLUDED.kb, title = EXCLUDED.title, classification = EXCLUDED.classification,
				     severity = EXCLUDED.severity, size_bytes = EXCLUDED.size_bytes, published_at = COALESCE(EXCLUDED.published_at, updates.published_at),
				     security = EXCLUDED.security`,
				platform, u.ID, u.KB, u.Title, u.Classification, u.Severity, u.SizeBytes, u.PublishedAt, u.Security, now); err != nil {
				return fmt.Errorf("catálogo de actualizaciones: %w", err)
			}
			if _, err := q.Exec(ctx,
				`INSERT INTO device_updates (device_id, platform, update_id, state, first_seen_at) VALUES ($1, $2, $3, 'missing', $4)
				 ON CONFLICT (device_id, update_id) DO UPDATE SET state = CASE WHEN device_updates.state = 'failed' THEN 'failed' ELSE 'missing' END`,
				deviceID, platform, u.ID, now); err != nil {
				return err
			}
		}
		// Las que ya no aparecen se han instalado (o retirado): dejan de estar pendientes.
		if _, err := q.Exec(ctx,
			`UPDATE device_updates SET state = 'installed', installed_at = COALESCE(installed_at, $3)
			 WHERE device_id = $1 AND state <> 'installed' AND NOT (update_id = ANY($2))`, deviceID, ids, now); err != nil {
			return err
		}
		_, err := q.Exec(ctx,
			`INSERT INTO device_patch_state (device_id, last_scan_at, reboot_pending) VALUES ($1, $2, $3)
			 ON CONFLICT (device_id) DO UPDATE SET last_scan_at = EXCLUDED.last_scan_at, reboot_pending = EXCLUDED.reboot_pending`,
			deviceID, now, scan.RebootPending)
		return err
	})
}

// SaveUpdateResults guarda el resultado de una instalación.
func (s *Store) SaveUpdateResults(ctx context.Context, deviceID uuid.UUID, res protocol.UpdateResults, now time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		for _, r := range res.Results {
			state := "failed"
			if r.Status == "installed" {
				state = "installed"
			}
			if _, err := q.Exec(ctx,
				`UPDATE device_updates SET state = $3, last_error = $4, installed_at = CASE WHEN $3 = 'installed' THEN $5 ELSE installed_at END
				 WHERE device_id = $1 AND update_id = $2`, deviceID, r.ID, state, CleanText(r.Error, 1000), now); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx,
			`INSERT INTO device_patch_state (device_id, last_install_at, reboot_pending) VALUES ($1, $2, $3)
			 ON CONFLICT (device_id) DO UPDATE SET last_install_at = EXCLUDED.last_install_at, reboot_pending = EXCLUDED.reboot_pending`,
			deviceID, now, res.RebootPending)
		return err
	})
}

// DeviceUpdates devuelve las actualizaciones conocidas del equipo y su estado de parches.
func (s *Store) DeviceUpdates(ctx context.Context, deviceID uuid.UUID) ([]UpdateRow, PatchState, error) {
	var st PatchState
	err := s.pool.QueryRow(ctx, `SELECT last_scan_at, last_install_at, reboot_pending FROM device_patch_state WHERE device_id = $1`, deviceID).
		Scan(&st.LastScanAt, &st.LastInstallAt, &st.RebootPending)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, st, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.kb, u.title, u.classification, u.severity, u.size_bytes, u.published_at, u.security,
		        d.state, d.first_seen_at, d.installed_at, d.last_error
		 FROM device_updates d JOIN updates u ON u.platform = d.platform AND u.id = d.update_id
		 WHERE d.device_id = $1 ORDER BY (d.state = 'installed'), u.security DESC, u.title`, deviceID)
	if err != nil {
		return nil, st, err
	}
	defer rows.Close()
	out := []UpdateRow{}
	for rows.Next() {
		var r UpdateRow
		if err := rows.Scan(&r.ID, &r.KB, &r.Title, &r.Classification, &r.Severity, &r.SizeBytes, &r.PublishedAt, &r.Security,
			&r.State, &r.FirstSeenAt, &r.InstalledAt, &r.LastError); err != nil {
			return nil, st, err
		}
		out = append(out, r)
	}
	return out, st, rows.Err()
}

// UpdateDecisions devuelve las decisiones manuales por id de actualización.
func (s *Store) UpdateDecisions(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT update_id, decision FROM update_approvals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, d string
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

// SetUpdateDecision guarda (o borra, con decision vacía) una decisión manual.
func (s *Store) SetUpdateDecision(ctx context.Context, updateID, decision string, by uuid.UUID, at time.Time) error {
	if decision == "" {
		_, err := s.pool.Exec(ctx, `DELETE FROM update_approvals WHERE update_id = $1`, updateID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO update_approvals (update_id, decision, decided_by, decided_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (update_id) DO UPDATE SET decision = EXCLUDED.decision, decided_by = EXCLUDED.decided_by, decided_at = EXCLUDED.decided_at`,
		updateID, decision, by, at)
	return err
}

type UpdateSummary struct {
	UpdateRow
	Platform  string
	Missing   int
	Installed int
	Failed    int
	Decision  *string
}

type UpdateFilter struct {
	Platform, Classification, Severity, Query string
	Page, PerPage                             int
}

// ListUpdates devuelve las actualizaciones conocidas con cuántos equipos las necesitan.
func (s *Store) ListUpdates(ctx context.Context, f UpdateFilter) ([]UpdateSummary, int, error) {
	page, perPage := normalizePage(f.Page, f.PerPage)
	where := `($1 = '' OR u.platform = $1) AND ($2 = '' OR u.classification = $2) AND ($3 = '' OR u.severity = $3)
		AND ($4 = '' OR u.title ILIKE '%' || $4 || '%' OR u.kb ILIKE '%' || $4 || '%' OR u.id = $4)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM updates u WHERE `+where, f.Platform, f.Classification, f.Severity, f.Query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT u.platform, u.id, u.kb, u.title, u.classification, u.severity, u.size_bytes, u.published_at, u.security, u.first_seen_at,
		        count(*) FILTER (WHERE d.state = 'missing'), count(*) FILTER (WHERE d.state = 'installed'), count(*) FILTER (WHERE d.state = 'failed'),
		        a.decision
		 FROM updates u LEFT JOIN device_updates d ON d.platform = u.platform AND d.update_id = u.id
		 LEFT JOIN update_approvals a ON a.update_id = u.id
		 WHERE `+where+`
		 GROUP BY u.platform, u.id, a.decision
		 ORDER BY count(*) FILTER (WHERE d.state IN ('missing', 'failed')) DESC, u.security DESC, u.title
		 LIMIT $5 OFFSET $6`, f.Platform, f.Classification, f.Severity, f.Query, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []UpdateSummary{}
	for rows.Next() {
		var u UpdateSummary
		if err := rows.Scan(&u.Platform, &u.ID, &u.KB, &u.Title, &u.Classification, &u.Severity, &u.SizeBytes, &u.PublishedAt, &u.Security,
			&u.FirstSeenAt, &u.Missing, &u.Installed, &u.Failed, &u.Decision); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// ---------- Políticas de parches ----------

type PatchPolicy struct {
	ID            uuid.UUID
	Name          string
	Platform      string
	Priority      int
	AutoApprove   []string
	DeferDays     int
	Window        protocol.Window
	Reboot        string
	NotifyMinutes int
	LinuxScope    string
	ExcludedKBs   []string
	Assignments   []PolicyAssignment
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const patchPolicyCols = `id, name, platform, priority, auto_approve, defer_days, maint_window, reboot, notify_minutes, linux_scope, excluded_kbs, created_at, updated_at`

func scanPatchPolicy(row pgx.Row) (PatchPolicy, error) {
	var p PatchPolicy
	var win []byte
	err := row.Scan(&p.ID, &p.Name, &p.Platform, &p.Priority, &p.AutoApprove, &p.DeferDays, &win, &p.Reboot, &p.NotifyMinutes,
		&p.LinuxScope, &p.ExcludedKBs, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err == nil {
		_ = json.Unmarshal(win, &p.Window)
	}
	return p, err
}

func (s *Store) CreatePatchPolicy(ctx context.Context, p PatchPolicy) (PatchPolicy, error) {
	win, _ := json.Marshal(p.Window)
	out, err := scanPatchPolicy(s.pool.QueryRow(ctx,
		`INSERT INTO patch_policies (name, platform, priority, auto_approve, defer_days, maint_window, reboot, notify_minutes, linux_scope, excluded_kbs)
		 VALUES ($1, $2, (SELECT COALESCE(max(priority), 0) + 1 FROM patch_policies), $3, $4, $5, $6, $7, $8, $9) RETURNING `+patchPolicyCols,
		p.Name, p.Platform, nonNil(p.AutoApprove), p.DeferDays, win, p.Reboot, p.NotifyMinutes, p.LinuxScope, nonNil(p.ExcludedKBs)))
	if isUniqueViolation(err) {
		return out, ErrConflict
	}
	out.Assignments = []PolicyAssignment{}
	return out, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Store) UpdatePatchPolicy(ctx context.Context, p PatchPolicy, at time.Time) error {
	win, _ := json.Marshal(p.Window)
	tag, err := s.pool.Exec(ctx,
		`UPDATE patch_policies SET name = $2, auto_approve = $3, defer_days = $4, maint_window = $5, reboot = $6, notify_minutes = $7,
		     linux_scope = $8, excluded_kbs = $9, updated_at = $10 WHERE id = $1`,
		p.ID, p.Name, nonNil(p.AutoApprove), p.DeferDays, win, p.Reboot, p.NotifyMinutes, p.LinuxScope, nonNil(p.ExcludedKBs), at)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePatchPolicy(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM patch_policies WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) loadPatchAssignments(ctx context.Context, ps []PatchPolicy) error {
	if len(ps) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(ps))
	for i := range ps {
		idx[ps[i].ID], ids[i] = i, ps[i].ID
		ps[i].Assignments = []PolicyAssignment{}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.policy_id, a.group_id, COALESCE(g.name, ''), a.exclude FROM patch_policy_assignments a
		 LEFT JOIN groups g ON g.id = a.group_id WHERE a.policy_id = ANY($1) ORDER BY a.exclude, lower(COALESCE(g.name, ''))`, ids)
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

func (s *Store) GetPatchPolicy(ctx context.Context, id uuid.UUID) (PatchPolicy, error) {
	p, err := scanPatchPolicy(s.pool.QueryRow(ctx, `SELECT `+patchPolicyCols+` FROM patch_policies WHERE id = $1`, id))
	if err != nil {
		return p, err
	}
	ps := []PatchPolicy{p}
	err = s.loadPatchAssignments(ctx, ps)
	return ps[0], err
}

func (s *Store) ListPatchPolicies(ctx context.Context, platform string) ([]PatchPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+patchPolicyCols+` FROM patch_policies WHERE $1 = '' OR platform = $1 ORDER BY priority`, platform)
	if err != nil {
		return nil, err
	}
	out := []PatchPolicy{}
	for rows.Next() {
		p, err := scanPatchPolicy(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	return out, s.loadPatchAssignments(ctx, out)
}

func (s *Store) SetPatchPolicyAssignments(ctx context.Context, id uuid.UUID, as []policy.Assignment, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		tag, err := q.Exec(ctx, `UPDATE patch_policies SET updated_at = $2 WHERE id = $1`, id, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := q.Exec(ctx, `DELETE FROM patch_policy_assignments WHERE policy_id = $1`, id); err != nil {
			return err
		}
		for _, a := range as {
			if _, err := q.Exec(ctx, `INSERT INTO patch_policy_assignments (policy_id, group_id, exclude) VALUES ($1, $2, $3)`, id, a.GroupID, a.Exclude); err != nil {
				if isForeignKeyViolation(err) {
					return ErrBadReference
				}
				return err
			}
		}
		return nil
	})
}

func (s *Store) ReorderPatchPolicies(ctx context.Context, ids []uuid.UUID) error {
	return s.Tx(ctx, func(q Querier) error {
		var total int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM patch_policies`).Scan(&total); err != nil {
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
			if _, err := q.Exec(ctx, `UPDATE patch_policies SET priority = $2 WHERE id = $1`, id, i+1); err != nil {
				return err
			}
		}
		return nil
	})
}

// MissingUpdates devuelve las actualizaciones pendientes (o fallidas) del equipo.
func (s *Store) MissingUpdates(ctx context.Context, deviceID uuid.UUID) ([]patches.Missing, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.kb, u.classification, u.security, u.published_at, d.first_seen_at
		 FROM device_updates d JOIN updates u ON u.platform = d.platform AND u.id = d.update_id
		 WHERE d.device_id = $1 AND d.state IN ('missing', 'failed')`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []patches.Missing
	for rows.Next() {
		var m patches.Missing
		if err := rows.Scan(&m.ID, &m.KB, &m.Classification, &m.Security, &m.PublishedAt, &m.FirstSeenAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// OldestApprovedSecurityUpdate devuelve desde cuándo el equipo tiene pendiente
// la actualización de seguridad aprobada más antigua (nil si no tiene ninguna).
func (s *Store) OldestApprovedSecurityUpdate(ctx context.Context, deviceID uuid.UUID, approved []string) (*time.Time, error) {
	if len(approved) == 0 {
		return nil, nil
	}
	var t *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT min(d.first_seen_at) FROM device_updates d JOIN updates u ON u.platform = d.platform AND u.id = d.update_id
		 WHERE d.device_id = $1 AND d.state IN ('missing', 'failed') AND u.security AND d.update_id = ANY($2)`, deviceID, approved).Scan(&t)
	return t, err
}

func init() {
	effectivePatches = func(ctx context.Context, s *Store, deviceID uuid.UUID, platform string, groups []uuid.UUID, eff *protocol.EffectivePolicy) error {
		pols, err := s.ListPatchPolicies(ctx, platform)
		if err != nil || len(pols) == 0 {
			return err
		}
		sort.Slice(pols, func(i, j int) bool { return pols[i].Priority < pols[j].Priority })
		for _, p := range pols {
			ap := toAssigned(Policy{Platform: p.Platform, Assignments: p.Assignments})
			if !policy.Applies(ap, platform, groups) {
				continue
			}
			missing, err := s.MissingUpdates(ctx, deviceID)
			if err != nil {
				return err
			}
			decisions, err := s.UpdateDecisions(ctx)
			if err != nil {
				return err
			}
			approved := patches.Approved(missing, patches.Policy{Platform: platform, AutoApprove: p.AutoApprove, DeferDays: p.DeferDays,
				ExcludedKBs: p.ExcludedKBs, LinuxScope: p.LinuxScope}, decisions, time.Now())
			eff.Patches = &protocol.PatchPolicy{Window: p.Window, Reboot: p.Reboot, NotifyMinutes: p.NotifyMinutes,
				LinuxScope: p.LinuxScope, ApprovedUpdateIDs: approved}
			return nil
		}
		return nil
	}
}
