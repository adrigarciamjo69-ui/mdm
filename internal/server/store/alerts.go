package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/server/policy"
)

type AlertRule struct {
	ID             uuid.UUID
	Name           string
	Type           string
	Params         json.RawMessage
	Severity       string
	Channels       []uuid.UUID
	Emails         []string
	NotifyResolved bool
	Enabled        bool
	Assignments    []PolicyAssignment
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const alertRuleCols = `id, name, type, params, severity, channels, emails, notify_resolved, enabled, created_at, updated_at`

func scanAlertRule(row pgx.Row) (AlertRule, error) {
	var r AlertRule
	err := row.Scan(&r.ID, &r.Name, &r.Type, &r.Params, &r.Severity, &r.Channels, &r.Emails, &r.NotifyResolved, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func setRuleAssignments(ctx context.Context, q Querier, id uuid.UUID, as []policy.Assignment) error {
	if _, err := q.Exec(ctx, `DELETE FROM alert_rule_assignments WHERE rule_id = $1`, id); err != nil {
		return err
	}
	for _, a := range as {
		if _, err := q.Exec(ctx, `INSERT INTO alert_rule_assignments (rule_id, group_id, exclude) VALUES ($1, $2, $3)`, id, a.GroupID, a.Exclude); err != nil {
			if isForeignKeyViolation(err) {
				return ErrBadReference
			}
			return err
		}
	}
	return nil
}

func (s *Store) CreateAlertRule(ctx context.Context, r AlertRule, as []policy.Assignment) (AlertRule, error) {
	var id uuid.UUID
	err := s.Tx(ctx, func(q Querier) error {
		if err := q.QueryRow(ctx,
			`INSERT INTO alert_rules (name, type, params, severity, channels, emails, notify_resolved, enabled) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			r.Name, r.Type, r.Params, r.Severity, nonNilUUID(r.Channels), nonNil(r.Emails), r.NotifyResolved, r.Enabled).Scan(&id); err != nil {
			return err
		}
		return setRuleAssignments(ctx, q, id, as)
	})
	if isUniqueViolation(err) {
		return AlertRule{}, ErrConflict
	}
	if err != nil {
		return AlertRule{}, err
	}
	return s.GetAlertRule(ctx, id)
}

func nonNilUUID(v []uuid.UUID) []uuid.UUID {
	if v == nil {
		return []uuid.UUID{}
	}
	return v
}

// UpdateAlertRule guarda la regla (asignaciones nil = sin cambios). Si queda
// desactivada, sus alertas sin resolver se resuelven.
func (s *Store) UpdateAlertRule(ctx context.Context, r AlertRule, as []policy.Assignment, at time.Time) error {
	err := s.Tx(ctx, func(q Querier) error {
		tag, err := q.Exec(ctx,
			`UPDATE alert_rules SET name = $2, params = $3, severity = $4, channels = $5, emails = $6, notify_resolved = $7, enabled = $8, updated_at = $9 WHERE id = $1`,
			r.ID, r.Name, r.Params, r.Severity, nonNilUUID(r.Channels), nonNil(r.Emails), r.NotifyResolved, r.Enabled, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if as != nil {
			if err := setRuleAssignments(ctx, q, r.ID, as); err != nil {
				return err
			}
		}
		if !r.Enabled {
			_, err = q.Exec(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = $2, notified_resolved = true WHERE rule_id = $1 AND state <> 'resolved'`, r.ID, at)
		}
		return err
	})
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeleteAlertRule resuelve sus alertas y borra la regla (las alertas se conservan).
func (s *Store) DeleteAlertRule(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		if _, err := q.Exec(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = $2, notified_resolved = true WHERE rule_id = $1 AND state <> 'resolved'`, id, at); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

func (s *Store) loadRuleAssignments(ctx context.Context, rs []AlertRule) error {
	if len(rs) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(rs))
	for i := range rs {
		idx[rs[i].ID], ids[i] = i, rs[i].ID
		rs[i].Assignments = []PolicyAssignment{}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.rule_id, a.group_id, COALESCE(g.name, ''), a.exclude FROM alert_rule_assignments a LEFT JOIN groups g ON g.id = a.group_id
		 WHERE a.rule_id = ANY($1) ORDER BY a.exclude, lower(COALESCE(g.name, ''))`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid uuid.UUID
		var a PolicyAssignment
		if err := rows.Scan(&rid, &a.GroupID, &a.GroupName, &a.Exclude); err != nil {
			return err
		}
		rs[idx[rid]].Assignments = append(rs[idx[rid]].Assignments, a)
	}
	return rows.Err()
}

func (s *Store) GetAlertRule(ctx context.Context, id uuid.UUID) (AlertRule, error) {
	r, err := scanAlertRule(s.pool.QueryRow(ctx, `SELECT `+alertRuleCols+` FROM alert_rules WHERE id = $1`, id))
	if err != nil {
		return r, err
	}
	rs := []AlertRule{r}
	err = s.loadRuleAssignments(ctx, rs)
	return rs[0], err
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertRuleCols+` FROM alert_rules ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	out := []AlertRule{}
	for rows.Next() {
		r, err := scanAlertRule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	return out, s.loadRuleAssignments(ctx, out)
}

// ScopedDevice es un equipo no revocado del alcance de una regla.
type ScopedDevice struct {
	ID       uuid.UUID
	Hostname string
	Platform string
}

// DevicesInScope devuelve los equipos no revocados a los que aplican las asignaciones.
func (s *Store) DevicesInScope(ctx context.Context, as []PolicyAssignment) (map[uuid.UUID]ScopedDevice, error) {
	ap := policy.AssignedPolicy{}
	for _, a := range as {
		ap.Assignments = append(ap.Assignments, policy.Assignment{GroupID: a.GroupID, Exclude: a.Exclude})
	}
	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.hostname, d.platform, d.group_id, ARRAY(SELECT m.group_id FROM group_members m WHERE m.device_id = d.id)
		 FROM devices d WHERE d.revoked_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]ScopedDevice{}
	for rows.Next() {
		var d ScopedDevice
		var gid *uuid.UUID
		var groups []uuid.UUID
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Platform, &gid, &groups); err != nil {
			return nil, err
		}
		if gid != nil {
			groups = append(groups, *gid)
		}
		ap.Platform = d.Platform
		if policy.Applies(ap, d.Platform, groups) {
			out[d.ID] = d
		}
	}
	return out, rows.Err()
}

// Match es un equipo que cumple la condición de una regla de estado.
type Match struct {
	DeviceID uuid.UUID
	Detail   map[string]any
}

func (s *Store) queryMatches(ctx context.Context, sql string, args ...any) ([]Match, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Match
	for rows.Next() {
		var m Match
		var detail []byte
		if err := rows.Scan(&m.DeviceID, &detail); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detail, &m.Detail)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Condiciones de las reglas de estado (solo equipos no revocados).
func (s *Store) OfflineDevices(ctx context.Context, cutoff time.Time) ([]Match, error) {
	return s.queryMatches(ctx, `SELECT id, jsonb_build_object('last_seen_at', last_seen_at) FROM devices
		WHERE revoked_at IS NULL AND COALESCE(last_seen_at, enrolled_at) < $1`, cutoff)
}

func (s *Store) LowDiskDevices(ctx context.Context, ratio float64) ([]Match, error) {
	return s.queryMatches(ctx, `SELECT d.id, (SELECT jsonb_build_object('mount', x->>'mount', 'free_percent',
			round((x->>'free_bytes')::numeric * 100 / (x->>'size_bytes')::numeric))
		 FROM device_inventory i, jsonb_array_elements(i.data->'disks') x WHERE i.device_id = d.id AND (x->>'size_bytes')::numeric > 0
		 ORDER BY (x->>'free_bytes')::numeric / (x->>'size_bytes')::numeric LIMIT 1)
		FROM devices d WHERE `+fmt.Sprintf(sqlLowDisk, ratio))
}

func (s *Store) NoncompliantDevices(ctx context.Context, policyID *uuid.UUID) ([]Match, error) {
	if policyID != nil {
		return s.queryMatches(ctx, `SELECT DISTINCT r.device_id, '{}'::jsonb FROM device_setting_results r JOIN devices d ON d.id = r.device_id
			JOIN device_compliance c ON c.device_id = d.id
			WHERE d.revoked_at IS NULL AND c.state = 'noncompliant' AND r.policy_id = $1 AND r.status IN ('noncompliant', 'error')`, *policyID)
	}
	return s.queryMatches(ctx, `SELECT c.device_id, jsonb_build_object('reasons', c.reasons) FROM device_compliance c JOIN devices d ON d.id = c.device_id
		WHERE d.revoked_at IS NULL AND c.state = 'noncompliant'`)
}

func (s *Store) AndroidIntegrityDevices(ctx context.Context) ([]Match, error) {
	return s.queryMatches(ctx, `SELECT d.id, jsonb_build_object('integrity', i.data->'mobile'->>'integrity') FROM devices d JOIN device_inventory i ON i.device_id = d.id
		WHERE d.revoked_at IS NULL AND d.platform = 'android' AND i.data->'mobile'->>'integrity' IN ('AT_RISK', 'POTENTIALLY_COMPROMISED')`)
}

func (s *Store) SecurityPendingDevices(ctx context.Context, cutoff time.Time) ([]Match, error) {
	return s.queryMatches(ctx, `SELECT u.device_id, jsonb_build_object('count', count(*), 'oldest', min(u.first_seen_at))
		FROM device_updates u JOIN updates x ON x.platform = u.platform AND x.id = u.update_id JOIN devices d ON d.id = u.device_id
		WHERE d.revoked_at IS NULL AND x.security AND u.state IN ('missing', 'failed') AND u.first_seen_at < $1 GROUP BY u.device_id`, cutoff)
}

func (s *Store) BitlockerKeyMissing(ctx context.Context) ([]Match, error) {
	return s.queryMatches(ctx, `SELECT d.id, '{}'::jsonb FROM devices d JOIN device_inventory i ON i.device_id = d.id
		WHERE d.revoked_at IS NULL AND d.platform = 'windows' AND i.data->'security'->>'disk_encryption' = 'on'
		  AND NOT EXISTS (SELECT 1 FROM recovery_keys k WHERE k.device_id = d.id)`)
}

func (s *Store) CountAuditSince(ctx context.Context, action string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1 AND at >= $2`, action, since).Scan(&n)
	return n, err
}

// ---------- Alertas ----------

type Alert struct {
	ID             int64
	RuleID         *uuid.UUID
	RuleName       string
	DeviceID       *uuid.UUID
	Hostname       string
	Platform       string
	State          string
	Severity       string
	Title          string
	Details        json.RawMessage
	OpenedAt       time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy *uuid.UUID
	ResolvedAt     *time.Time
	ResolvedBy     *uuid.UUID
}

const alertCols = `a.id, a.rule_id, a.rule_name, a.device_id, COALESCE(d.hostname, ''), COALESCE(d.platform, ''), a.state, a.severity, a.title, a.details,
	a.opened_at, a.acknowledged_at, a.acknowledged_by, a.resolved_at, a.resolved_by`

func scanAlert(row pgx.Row) (Alert, error) {
	var a Alert
	err := row.Scan(&a.ID, &a.RuleID, &a.RuleName, &a.DeviceID, &a.Hostname, &a.Platform, &a.State, &a.Severity, &a.Title, &a.Details,
		&a.OpenedAt, &a.AcknowledgedAt, &a.AcknowledgedBy, &a.ResolvedAt, &a.ResolvedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// OpenAlert abre una alerta si no hay ya una sin resolver de esa regla y
// equipo; publica alert.opened en la misma transacción.
func (s *Store) OpenAlert(ctx context.Context, a Alert) (int64, bool, error) {
	var id int64
	opened := false
	err := s.Tx(ctx, func(q Querier) error {
		if a.Details == nil {
			a.Details = json.RawMessage(`{}`)
		}
		err := q.QueryRow(ctx,
			`INSERT INTO alerts (rule_id, rule_name, device_id, severity, title, details, opened_at) VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (rule_id, COALESCE(device_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE state <> 'resolved' DO NOTHING
			 RETURNING id`, a.RuleID, a.RuleName, a.DeviceID, a.Severity, CleanText(a.Title, 500), a.Details, a.OpenedAt).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		opened = true
		data := map[string]any{"alert_id": id, "severity": a.Severity}
		if a.RuleID != nil {
			data["rule_id"] = a.RuleID.String()
		}
		return emit(ctx, q, Event{Type: EventAlertOpened, DeviceID: a.DeviceID, At: a.OpenedAt, Data: data})
	})
	return id, opened, err
}

// UnresolvedAlerts devuelve las alertas sin resolver de una regla.
func (s *Store) UnresolvedAlerts(ctx context.Context, ruleID uuid.UUID) ([]Alert, error) {
	return s.queryAlerts(ctx, `a.rule_id = $1 AND a.state <> 'resolved'`, ruleID)
}

func (s *Store) queryAlerts(ctx context.Context, where string, args ...any) ([]Alert, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertCols+` FROM alerts a LEFT JOIN devices d ON d.id = a.device_id WHERE `+where+` ORDER BY a.opened_at, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAlert(ctx context.Context, id int64) (Alert, error) {
	return scanAlert(s.pool.QueryRow(ctx, `SELECT `+alertCols+` FROM alerts a LEFT JOIN devices d ON d.id = a.device_id WHERE a.id = $1`, id))
}

// ResolveAlert resuelve una alerta (by nulo = se resolvió sola).
func (s *Store) ResolveAlert(ctx context.Context, id int64, by *uuid.UUID, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = $2, resolved_by = $3 WHERE id = $1 AND state <> 'resolved'`, id, at, by)
	return tag.RowsAffected() > 0, err
}

func (s *Store) AcknowledgeAlert(ctx context.Context, id int64, by uuid.UUID, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE alerts SET state = 'acknowledged', acknowledged_at = $2, acknowledged_by = $3 WHERE id = $1 AND state = 'open'`, id, at, by)
	return tag.RowsAffected() > 0, err
}

// ResolveOldEventAlerts cierra las alertas de reglas de suceso abiertas antes de before.
func (s *Store) ResolveOldEventAlerts(ctx context.Context, eventTypes []string, before, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE alerts a SET state = 'resolved', resolved_at = $3, notified_resolved = true
		FROM alert_rules r WHERE r.id = a.rule_id AND r.type = ANY($1) AND a.state <> 'resolved' AND a.opened_at < $2`, eventTypes, before, at)
	return tag.RowsAffected(), err
}

// PendingNotifications devuelve las alertas de la regla de las que aún no se ha avisado.
func (s *Store) PendingNotifications(ctx context.Context, ruleID uuid.UUID, kind string) ([]Alert, error) {
	if kind == "resolved" {
		return s.queryAlerts(ctx, `a.rule_id = $1 AND a.state = 'resolved' AND NOT a.notified_resolved AND a.notified_open`, ruleID)
	}
	return s.queryAlerts(ctx, `a.rule_id = $1 AND NOT a.notified_open`, ruleID)
}

func (s *Store) MarkNotified(ctx context.Context, ids []int64, kind string) error {
	col := "notified_open"
	if kind == "resolved" {
		col = "notified_resolved"
	}
	_, err := s.pool.Exec(ctx, `UPDATE alerts SET `+col+` = true WHERE id = ANY($1)`, ids)
	return err
}

type AlertFilter struct {
	State    string // open, acknowledged, resolved, unresolved (open + acknowledged) o vacío
	Severity string
	RuleID   *uuid.UUID
	DeviceID *uuid.UUID
	Page     int
	PerPage  int
}

func (s *Store) ListAlerts(ctx context.Context, f AlertFilter) ([]Alert, int, error) {
	page, perPage := normalizePage(f.Page, f.PerPage)
	where := `($1 = '' OR ($1 = 'unresolved' AND a.state <> 'resolved') OR a.state = $1) AND ($2 = '' OR a.severity = $2)
		AND ($3::uuid IS NULL OR a.rule_id = $3) AND ($4::uuid IS NULL OR a.device_id = $4)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alerts a WHERE `+where, f.State, f.Severity, f.RuleID, f.DeviceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+alertCols+` FROM alerts a LEFT JOIN devices d ON d.id = a.device_id WHERE `+where+`
		ORDER BY (a.state = 'resolved'), CASE a.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, a.opened_at DESC, a.id DESC
		LIMIT $5 OFFSET $6`, f.State, f.Severity, f.RuleID, f.DeviceID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// OpenAlertCounts cuenta las alertas sin resolver por gravedad.
func (s *Store) OpenAlertCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT severity, count(*) FROM alerts WHERE state <> 'resolved' GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"critical": 0, "warning": 0, "info": 0}
	for rows.Next() {
		var sev string
		var n int
		if err := rows.Scan(&sev, &n); err != nil {
			return nil, err
		}
		out[sev] = n
	}
	return out, rows.Err()
}
