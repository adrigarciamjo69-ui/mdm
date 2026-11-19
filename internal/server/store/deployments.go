package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
	"mdm/internal/server/policy"
)

type Deployment struct {
	ID            uuid.UUID
	AppID         uuid.UUID
	AppName       string
	AppSource     string
	AppPlatform   string
	Intent        string
	KeepUpdated   bool
	StartAt       *time.Time
	RespectWindow bool
	Reboot        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Assignments   []PolicyAssignment
	Counts        map[string]int
}

const deploymentCols = `d.id, d.app_id, a.name, a.source, a.platform, d.intent, d.keep_updated, d.start_at, d.respect_window, d.reboot, d.created_at, d.updated_at`

func scanDeployment(row pgx.Row) (Deployment, error) {
	var d Deployment
	err := row.Scan(&d.ID, &d.AppID, &d.AppName, &d.AppSource, &d.AppPlatform, &d.Intent, &d.KeepUpdated, &d.StartAt,
		&d.RespectWindow, &d.Reboot, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (s *Store) CreateDeployment(ctx context.Context, d Deployment, by uuid.UUID) (Deployment, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO deployments (app_id, intent, keep_updated, start_at, respect_window, reboot, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		d.AppID, d.Intent, d.KeepUpdated, d.StartAt, d.RespectWindow, d.Reboot, by).Scan(&id)
	if isForeignKeyViolation(err) {
		return d, ErrBadReference
	}
	if err != nil {
		return d, fmt.Errorf("crear despliegue: %w", err)
	}
	return s.GetDeployment(ctx, id)
}

func (s *Store) loadDeploymentAssignments(ctx context.Context, ds []Deployment) error {
	if len(ds) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(ds))
	for i := range ds {
		idx[ds[i].ID], ids[i] = i, ds[i].ID
		ds[i].Assignments = []PolicyAssignment{}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.deployment_id, a.group_id, COALESCE(g.name, ''), a.exclude FROM deployment_assignments a
		 LEFT JOIN groups g ON g.id = a.group_id WHERE a.deployment_id = ANY($1) ORDER BY a.exclude, lower(COALESCE(g.name, ''))`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var did uuid.UUID
		var a PolicyAssignment
		if err := rows.Scan(&did, &a.GroupID, &a.GroupName, &a.Exclude); err != nil {
			return err
		}
		ds[idx[did]].Assignments = append(ds[idx[did]].Assignments, a)
	}
	return rows.Err()
}

func (s *Store) GetDeployment(ctx context.Context, id uuid.UUID) (Deployment, error) {
	d, err := scanDeployment(s.pool.QueryRow(ctx, `SELECT `+deploymentCols+` FROM deployments d JOIN apps a ON a.id = d.app_id WHERE d.id = $1`, id))
	if err != nil {
		return d, err
	}
	ds := []Deployment{d}
	err = s.loadDeploymentAssignments(ctx, ds)
	return ds[0], err
}

// ListDeployments devuelve todos los despliegues; platform vacío = todos.
func (s *Store) ListDeployments(ctx context.Context, platform string) ([]Deployment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deploymentCols+` FROM deployments d JOIN apps a ON a.id = d.app_id
		WHERE $1 = '' OR a.platform = $1 ORDER BY lower(a.name), d.created_at`, platform)
	if err != nil {
		return nil, err
	}
	out := []Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, d)
	}
	rows.Close()
	return out, s.loadDeploymentAssignments(ctx, out)
}

func (s *Store) UpdateDeployment(ctx context.Context, d Deployment, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE deployments SET intent = $2, keep_updated = $3, start_at = $4, respect_window = $5, reboot = $6, updated_at = $7 WHERE id = $1`,
		d.ID, d.Intent, d.KeepUpdated, d.StartAt, d.RespectWindow, d.Reboot, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDeployment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM deployments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetDeploymentAssignments(ctx context.Context, id uuid.UUID, as []policy.Assignment, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		tag, err := q.Exec(ctx, `UPDATE deployments SET updated_at = $2 WHERE id = $1`, id, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := q.Exec(ctx, `DELETE FROM deployment_assignments WHERE deployment_id = $1`, id); err != nil {
			return err
		}
		for _, a := range as {
			if _, err := q.Exec(ctx, `INSERT INTO deployment_assignments (deployment_id, group_id, exclude) VALUES ($1, $2, $3)`, id, a.GroupID, a.Exclude); err != nil {
				if isForeignKeyViolation(err) {
					return ErrBadReference
				}
				return err
			}
		}
		return nil
	})
}

// AssignedDeployments carga los despliegues de una plataforma con su
// aplicación y su versión vigente.
func (s *Store) AssignedDeployments(ctx context.Context, platform string) ([]policy.AssignedDeployment, error) {
	ds, err := s.ListDeployments(ctx, platform)
	if err != nil || len(ds) == 0 {
		return nil, err
	}
	appIDs := []uuid.UUID{}
	for _, d := range ds {
		appIDs = append(appIDs, d.AppID)
	}
	versions, err := s.appVersions(ctx, appIDs)
	if err != nil {
		return nil, err
	}
	pkgs := map[uuid.UUID]string{}
	rows, err := s.pool.Query(ctx, `SELECT id, package_id FROM apps WHERE id = ANY($1)`, appIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return nil, err
		}
		pkgs[id] = p
	}
	rows.Close()
	out := make([]policy.AssignedDeployment, 0, len(ds))
	for _, d := range ds {
		ad := policy.AssignedDeployment{ID: d.ID, AppID: d.AppID.String(), AppName: d.AppName, Platform: d.AppPlatform, Intent: d.Intent,
			Source: d.AppSource, PackageID: pkgs[d.AppID], KeepUpdated: d.KeepUpdated, StartAt: d.StartAt, RespectWindow: d.RespectWindow,
			Reboot: d.Reboot, CreatedAt: d.CreatedAt}
		for _, a := range d.Assignments {
			ad.Assignments = append(ad.Assignments, policy.Assignment{GroupID: a.GroupID, Exclude: a.Exclude})
		}
		for _, v := range versions[d.AppID] {
			if v.IsCurrent {
				det := v.Detection
				ad.Version = &protocol.AppAssignment{Version: v.Version, FileSHA256: v.FileSHA256, Size: v.FileSize, InstallerType: v.InstallerType,
					InstallCommand: v.InstallCommand, UninstallCommand: v.UninstallCommand, SuccessCodes: v.SuccessCodes, Detection: &det}
			}
		}
		out = append(out, ad)
	}
	return out, nil
}

// retryEpochs devuelve la época de reintento de cada despliegue en el equipo.
func (s *Store) retryEpochs(ctx context.Context, deviceID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT deployment_id, retry_epoch FROM deployment_status WHERE device_id = $1 AND retry_epoch > 0`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var e int
		if err := rows.Scan(&id, &e); err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, rows.Err()
}

type DeploymentStatusRow struct {
	DeviceID       uuid.UUID
	Hostname       string
	DeploymentID   uuid.UUID
	AppName        string
	Intent         string
	State          string
	Message        string
	Attempts       int
	ExitCode       *int
	RebootRequired bool
	UpdatedAt      *time.Time
}

// SaveAppStatus guarda el estado que informa el agente (conserva la época de reintento).
func (s *Store) SaveAppStatus(ctx context.Context, deviceID uuid.UUID, items []protocol.AppStatus) error {
	return s.Tx(ctx, func(q Querier) error {
		for _, it := range items {
			id, err := uuid.Parse(it.DeploymentID)
			if err != nil {
				continue
			}
			var prev string
			_ = q.QueryRow(ctx, `SELECT state FROM deployment_status WHERE device_id = $1 AND deployment_id = $2 FOR UPDATE`, deviceID, id).Scan(&prev)
			if it.State == "failed" && prev != "failed" {
				if err := emit(ctx, q, Event{Type: EventDeploymentFailed, DeviceID: devicePtr(deviceID), At: it.UpdatedAt,
					Data: map[string]any{"deployment_id": id.String(), "message": CleanText(it.Message, 500)}}); err != nil {
					return err
				}
			}
			if _, err := q.Exec(ctx,
				`INSERT INTO deployment_status (device_id, deployment_id, state, message, attempts, exit_code, reboot_required, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				 ON CONFLICT (device_id, deployment_id) DO UPDATE SET state = EXCLUDED.state, message = EXCLUDED.message,
				     attempts = EXCLUDED.attempts, exit_code = EXCLUDED.exit_code, reboot_required = EXCLUDED.reboot_required, updated_at = EXCLUDED.updated_at`,
				deviceID, id, it.State, CleanText(it.Message, 4096), it.Attempts, it.ExitCode, it.RebootRequired, it.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeviceDeploymentStatus devuelve los estados guardados de un equipo.
func (s *Store) DeviceDeploymentStatus(ctx context.Context, deviceID uuid.UUID) ([]DeploymentStatusRow, error) {
	return s.queryDeploymentStatus(ctx, `st.device_id = $1`, deviceID)
}

func (s *Store) queryDeploymentStatus(ctx context.Context, where string, arg any) ([]DeploymentStatusRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT st.device_id, dv.hostname, st.deployment_id, a.name, d.intent, st.state, st.message, st.attempts, st.exit_code, st.reboot_required, st.updated_at
		 FROM deployment_status st JOIN devices dv ON dv.id = st.device_id JOIN deployments d ON d.id = st.deployment_id JOIN apps a ON a.id = d.app_id
		 WHERE `+where+` ORDER BY lower(dv.hostname)`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeploymentStatusRow{}
	for rows.Next() {
		var r DeploymentStatusRow
		if err := rows.Scan(&r.DeviceID, &r.Hostname, &r.DeploymentID, &r.AppName, &r.Intent, &r.State, &r.Message, &r.Attempts,
			&r.ExitCode, &r.RebootRequired, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeploymentStatusByID devuelve los estados guardados de un despliegue.
func (s *Store) DeploymentStatusByID(ctx context.Context, id uuid.UUID) ([]DeploymentStatusRow, error) {
	return s.queryDeploymentStatus(ctx, `st.deployment_id = $1`, id)
}

// RetryDeployment reinicia los intentos de los equipos fallidos (o de uno) y
// sube su época de reintento, que cambia la huella de su política.
func (s *Store) RetryDeployment(ctx context.Context, id uuid.UUID, deviceID *uuid.UUID, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deployment_status SET state = 'pending', attempts = 0, message = '', retry_epoch = retry_epoch + 1, updated_at = $3
		 WHERE deployment_id = $1 AND ($2::uuid IS NULL OR device_id = $2) AND ($2::uuid IS NOT NULL OR state = 'failed')`, id, deviceID, at)
	return tag.RowsAffected(), err
}

// DeploymentCounts cuenta los estados guardados por despliegue.
func (s *Store) DeploymentCounts(ctx context.Context) (map[uuid.UUID]map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT deployment_id, state, count(*) FROM deployment_status GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]map[string]int{}
	for rows.Next() {
		var id uuid.UUID
		var st string
		var n int
		if err := rows.Scan(&id, &st, &n); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]int{}
		}
		out[id][st] = n
	}
	return out, rows.Err()
}
