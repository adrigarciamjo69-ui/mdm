package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

type App struct {
	ID          uuid.UUID
	Name        string
	Publisher   string
	Description string
	Platform    string
	Source      string
	PackageID   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
	Versions    []AppVersion
}

type AppVersion struct {
	ID               uuid.UUID
	AppID            uuid.UUID
	Version          string
	FileSHA256       string
	FileSize         int64
	FileName         string
	InstallerType    string
	InstallCommand   string
	UninstallCommand string
	SuccessCodes     []int
	Detection        protocol.Detection
	IsCurrent        bool
	CreatedAt        time.Time
}

// Current devuelve la versión vigente, si la hay.
func (a App) Current() *AppVersion {
	for i := range a.Versions {
		if a.Versions[i].IsCurrent {
			return &a.Versions[i]
		}
	}
	return nil
}

const appCols = `id, name, publisher, description, platform, source, package_id, created_at, updated_at, archived_at`

func scanApp(row pgx.Row) (App, error) {
	var a App
	err := row.Scan(&a.ID, &a.Name, &a.Publisher, &a.Description, &a.Platform, &a.Source, &a.PackageID, &a.CreatedAt, &a.UpdatedAt, &a.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateApp(ctx context.Context, a App, by uuid.UUID) (App, error) {
	out, err := scanApp(s.pool.QueryRow(ctx,
		`INSERT INTO apps (name, publisher, description, platform, source, package_id, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING `+appCols, a.Name, a.Publisher, a.Description, a.Platform, a.Source, a.PackageID, by))
	if isUniqueViolation(err) {
		return App{}, ErrConflict
	}
	if err != nil {
		return App{}, fmt.Errorf("crear aplicación: %w", err)
	}
	out.Versions = []AppVersion{}
	return out, nil
}

func (s *Store) appVersions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]AppVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT v.id, v.app_id, v.version, v.file_sha256, f.size, f.original_name, v.installer_type, v.install_command,
		        v.uninstall_command, v.success_codes, v.detection, v.is_current, v.created_at
		 FROM app_versions v JOIN files f ON f.sha256 = v.file_sha256
		 WHERE v.app_id = ANY($1) ORDER BY v.created_at DESC, v.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]AppVersion{}
	for rows.Next() {
		var v AppVersion
		var det []byte
		if err := rows.Scan(&v.ID, &v.AppID, &v.Version, &v.FileSHA256, &v.FileSize, &v.FileName, &v.InstallerType, &v.InstallCommand,
			&v.UninstallCommand, &v.SuccessCodes, &det, &v.IsCurrent, &v.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(det, &v.Detection)
		out[v.AppID] = append(out[v.AppID], v)
	}
	return out, rows.Err()
}

func (s *Store) GetApp(ctx context.Context, id uuid.UUID) (App, error) {
	a, err := scanApp(s.pool.QueryRow(ctx, `SELECT `+appCols+` FROM apps WHERE id = $1`, id))
	if err != nil {
		return a, err
	}
	vs, err := s.appVersions(ctx, []uuid.UUID{id})
	a.Versions = vs[id]
	if a.Versions == nil {
		a.Versions = []AppVersion{}
	}
	return a, err
}

// ListApps devuelve las aplicaciones (activas o archivadas) con sus versiones.
func (s *Store) ListApps(ctx context.Context, archived bool) ([]App, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+appCols+` FROM apps WHERE (archived_at IS NOT NULL) = $1 ORDER BY lower(name)`, archived)
	if err != nil {
		return nil, err
	}
	var out []App
	var ids []uuid.UUID
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
		ids = append(ids, a.ID)
	}
	rows.Close()
	vs, err := s.appVersions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Versions = vs[out[i].ID]
		if out[i].Versions == nil {
			out[i].Versions = []AppVersion{}
		}
	}
	if out == nil {
		out = []App{}
	}
	return out, nil
}

func (s *Store) UpdateApp(ctx context.Context, id uuid.UUID, name, publisher, description string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE apps SET name = $2, publisher = $3, description = $4, updated_at = $5 WHERE id = $1`, id, name, publisher, description, at)
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

func (s *Store) ArchiveApp(ctx context.Context, id uuid.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE apps SET archived_at = COALESCE(archived_at, $2) WHERE id = $1`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddAppVersion añade una versión; si current, pasa a ser la vigente.
func (s *Store) AddAppVersion(ctx context.Context, v AppVersion, current bool) (AppVersion, error) {
	det, _ := json.Marshal(v.Detection)
	err := s.Tx(ctx, func(q Querier) error {
		if current {
			if _, err := q.Exec(ctx, `UPDATE app_versions SET is_current = false WHERE app_id = $1 AND is_current`, v.AppID); err != nil {
				return err
			}
		}
		return q.QueryRow(ctx,
			`INSERT INTO app_versions (app_id, version, file_sha256, installer_type, install_command, uninstall_command, success_codes, detection, is_current)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, created_at`,
			v.AppID, v.Version, v.FileSHA256, v.InstallerType, v.InstallCommand, v.UninstallCommand, v.SuccessCodes, det, current).
			Scan(&v.ID, &v.CreatedAt)
	})
	switch {
	case isUniqueViolation(err):
		return v, ErrConflict
	case isForeignKeyViolation(err):
		return v, ErrBadReference
	}
	v.IsCurrent = current
	return v, err
}

// SetCurrentVersion marca una versión como vigente.
func (s *Store) SetCurrentVersion(ctx context.Context, appID, versionID uuid.UUID) error {
	return s.Tx(ctx, func(q Querier) error {
		var ok bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_versions WHERE id = $1 AND app_id = $2)`, versionID, appID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if _, err := q.Exec(ctx, `UPDATE app_versions SET is_current = false WHERE app_id = $1 AND is_current`, appID); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE app_versions SET is_current = true WHERE id = $1`, versionID)
		return err
	})
}

func init() {
	unreferencedFilesQuery = `SELECT sha256 FROM files f WHERE uploaded_at < $1
		AND NOT EXISTS (SELECT 1 FROM app_versions v WHERE v.file_sha256 = f.sha256)`
}
