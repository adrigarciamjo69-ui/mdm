package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/server/policy"
)

type Automation struct {
	ID          uuid.UUID
	Name        string
	Enabled     bool
	Trigger     json.RawMessage
	Actions     json.RawMessage
	MaxDevices  int
	LastRunAt   *time.Time
	CreatedBy   uuid.UUID
	Assignments []PolicyAssignment
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const automationCols = `id, name, enabled, trigger, actions, max_devices, last_run_at, COALESCE(created_by, '00000000-0000-0000-0000-000000000000'), created_at, updated_at`

func scanAutomation(row pgx.Row) (Automation, error) {
	var a Automation
	err := row.Scan(&a.ID, &a.Name, &a.Enabled, &a.Trigger, &a.Actions, &a.MaxDevices, &a.LastRunAt, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func setAutomationAssignments(ctx context.Context, q Querier, id uuid.UUID, as []policy.Assignment) error {
	if _, err := q.Exec(ctx, `DELETE FROM automation_assignments WHERE automation_id = $1`, id); err != nil {
		return err
	}
	for _, a := range as {
		if _, err := q.Exec(ctx, `INSERT INTO automation_assignments (automation_id, group_id, exclude) VALUES ($1, $2, $3)`, id, a.GroupID, a.Exclude); err != nil {
			if isForeignKeyViolation(err) {
				return ErrBadReference
			}
			return err
		}
	}
	return nil
}

// CreateAutomation crea la automatización; la creación cuenta como última
// ejecución (una programada no se ejecuta al crearla).
func (s *Store) CreateAutomation(ctx context.Context, a Automation, as []policy.Assignment, at time.Time) (Automation, error) {
	var id uuid.UUID
	err := s.Tx(ctx, func(q Querier) error {
		if err := q.QueryRow(ctx, `INSERT INTO automations (name, enabled, trigger, actions, max_devices, last_run_at, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			a.Name, a.Enabled, a.Trigger, a.Actions, a.MaxDevices, at, a.CreatedBy).Scan(&id); err != nil {
			return err
		}
		return setAutomationAssignments(ctx, q, id, as)
	})
	if isUniqueViolation(err) {
		return Automation{}, ErrConflict
	}
	if err != nil {
		return Automation{}, err
	}
	return s.GetAutomation(ctx, id)
}

func (s *Store) UpdateAutomation(ctx context.Context, a Automation, as []policy.Assignment, at time.Time) error {
	err := s.Tx(ctx, func(q Querier) error {
		tag, err := q.Exec(ctx, `UPDATE automations SET name = $2, enabled = $3, trigger = $4, actions = $5, max_devices = $6, updated_at = $7 WHERE id = $1`,
			a.ID, a.Name, a.Enabled, a.Trigger, a.Actions, a.MaxDevices, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if as != nil {
			return setAutomationAssignments(ctx, q, a.ID, as)
		}
		return nil
	})
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automations WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) loadAutomationAssignments(ctx context.Context, as []Automation) error {
	if len(as) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(as))
	for i := range as {
		idx[as[i].ID], ids[i] = i, as[i].ID
		as[i].Assignments = []PolicyAssignment{}
	}
	rows, err := s.pool.Query(ctx, `SELECT a.automation_id, a.group_id, COALESCE(g.name, ''), a.exclude FROM automation_assignments a
		LEFT JOIN groups g ON g.id = a.group_id WHERE a.automation_id = ANY($1) ORDER BY a.exclude, lower(COALESCE(g.name, ''))`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid uuid.UUID
		var x PolicyAssignment
		if err := rows.Scan(&aid, &x.GroupID, &x.GroupName, &x.Exclude); err != nil {
			return err
		}
		as[idx[aid]].Assignments = append(as[idx[aid]].Assignments, x)
	}
	return rows.Err()
}

func (s *Store) GetAutomation(ctx context.Context, id uuid.UUID) (Automation, error) {
	a, err := scanAutomation(s.pool.QueryRow(ctx, `SELECT `+automationCols+` FROM automations WHERE id = $1`, id))
	if err != nil {
		return a, err
	}
	list := []Automation{a}
	err = s.loadAutomationAssignments(ctx, list)
	return list[0], err
}

func (s *Store) ListAutomations(ctx context.Context) ([]Automation, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+automationCols+` FROM automations ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	out := []Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	return out, s.loadAutomationAssignments(ctx, out)
}

func (s *Store) SetAutomationLastRun(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE automations SET last_run_at = $2 WHERE id = $1`, id, at)
	return err
}

type AutomationRun struct {
	ID           int64
	AutomationID uuid.UUID
	TriggerInfo  json.RawMessage
	StartedAt    time.Time
	FinishedAt   *time.Time
	Status       string
	DeviceCount  int
	BatchIDs     []uuid.UUID
	Error        string
}

func (s *Store) AddAutomationRun(ctx context.Context, r AutomationRun) (int64, error) {
	var id int64
	if r.BatchIDs == nil {
		r.BatchIDs = []uuid.UUID{}
	}
	if r.TriggerInfo == nil {
		r.TriggerInfo = json.RawMessage(`{}`)
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO automation_runs (automation_id, trigger_info, started_at, finished_at, status, device_count, batch_ids, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, r.AutomationID, r.TriggerInfo, r.StartedAt, r.FinishedAt, r.Status, r.DeviceCount,
		r.BatchIDs, CleanText(r.Error, 1000)).Scan(&id)
	return id, err
}

func (s *Store) AutomationRuns(ctx context.Context, id uuid.UUID, limit int) ([]AutomationRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, automation_id, trigger_info, started_at, finished_at, status, device_count, batch_ids, error
		FROM automation_runs WHERE automation_id = $1 ORDER BY started_at DESC, id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRun{}
	for rows.Next() {
		var r AutomationRun
		if err := rows.Scan(&r.ID, &r.AutomationID, &r.TriggerInfo, &r.StartedAt, &r.FinishedAt, &r.Status, &r.DeviceCount, &r.BatchIDs, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TryFire registra el disparo por suceso si no hubo otro para ese equipo y
// suceso en la ventana; devuelve si se puede disparar.
func (s *Store) TryFire(ctx context.Context, automationID, deviceID uuid.UUID, event string, now time.Time, window time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO automation_fired (automation_id, device_id, event, fired_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (automation_id, device_id, event) DO UPDATE SET fired_at = EXCLUDED.fired_at
		WHERE automation_fired.fired_at <= $5`, automationID, deviceID, event, now, now.Add(-window))
	return tag.RowsAffected() > 0, err
}

// PurgeAutomationRuns borra el historial anterior a before (se conservan 90 días).
func (s *Store) PurgeAutomationRuns(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automation_runs WHERE started_at < $1`, before)
	return tag.RowsAffected(), err
}
