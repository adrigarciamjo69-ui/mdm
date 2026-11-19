package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ScriptParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
}

type Script struct {
	ID            uuid.UUID
	Name          string
	Description   string
	Platform      string
	Interpreter   string
	TimeoutS      int
	Parameters    []ScriptParam
	LatestVersion int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ArchivedAt    *time.Time
}

type ScriptVersion struct {
	Version        int
	Content        string
	CreatedByEmail string
	CreatedAt      time.Time
}

type NewScript struct {
	Name, Description, Platform, Interpreter, Content string
	TimeoutS                                          int
	Parameters                                        []ScriptParam
	CreatedBy                                         uuid.UUID
	CreatedByEmail                                    string
	At                                                time.Time
}

const scriptCols = `s.id, s.name, s.description, s.platform, s.interpreter, s.timeout_s, s.parameters, s.created_at, s.updated_at, s.archived_at,
	(SELECT COALESCE(max(version), 0) FROM script_versions v WHERE v.script_id = s.id)`

func scanScript(row pgx.Row) (Script, error) {
	var sc Script
	var params []byte
	err := row.Scan(&sc.ID, &sc.Name, &sc.Description, &sc.Platform, &sc.Interpreter, &sc.TimeoutS, &params, &sc.CreatedAt, &sc.UpdatedAt, &sc.ArchivedAt, &sc.LatestVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc, ErrNotFound
	}
	if err != nil {
		return sc, err
	}
	_ = json.Unmarshal(params, &sc.Parameters)
	if sc.Parameters == nil {
		sc.Parameters = []ScriptParam{}
	}
	return sc, nil
}

func paramsJSON(p []ScriptParam) []byte {
	if p == nil {
		p = []ScriptParam{}
	}
	b, _ := json.Marshal(p)
	return b
}

// CreateScript crea el script con su versión 1.
func (s *Store) CreateScript(ctx context.Context, n NewScript) (Script, error) {
	var id uuid.UUID
	err := s.Tx(ctx, func(q Querier) error {
		if err := q.QueryRow(ctx,
			`INSERT INTO scripts (name, description, platform, interpreter, timeout_s, parameters, created_by, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8) RETURNING id`,
			n.Name, n.Description, n.Platform, n.Interpreter, n.TimeoutS, paramsJSON(n.Parameters), n.CreatedBy, n.At).Scan(&id); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `INSERT INTO script_versions (script_id, version, content, created_by, created_by_email, created_at) VALUES ($1, 1, $2, $3, $4, $5)`,
			id, n.Content, n.CreatedBy, n.CreatedByEmail, n.At)
		return err
	})
	if isUniqueViolation(err) {
		return Script{}, ErrConflict
	}
	if err != nil {
		return Script{}, fmt.Errorf("crear script: %w", err)
	}
	return s.GetScript(ctx, id)
}

func (s *Store) GetScript(ctx context.Context, id uuid.UUID) (Script, error) {
	return scanScript(s.pool.QueryRow(ctx, `SELECT `+scriptCols+` FROM scripts s WHERE s.id = $1`, id))
}

func (s *Store) ListScripts(ctx context.Context, archived bool) ([]Script, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+scriptCols+` FROM scripts s WHERE (s.archived_at IS NOT NULL) = $1 ORDER BY lower(s.name)`, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Script{}
	for rows.Next() {
		sc, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// UpdateScript cambia los metadatos; el intérprete y la plataforma no cambian.
func (s *Store) UpdateScript(ctx context.Context, id uuid.UUID, name, description string, timeoutS int, params []ScriptParam, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE scripts SET name = $2, description = $3, timeout_s = $4, parameters = $5, updated_at = $6 WHERE id = $1`,
		id, name, description, timeoutS, paramsJSON(params), at)
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

// AddScriptVersion añade una versión y devuelve su número.
func (s *Store) AddScriptVersion(ctx context.Context, id uuid.UUID, content string, by uuid.UUID, byEmail string, at time.Time) (int, error) {
	var v int
	err := s.Tx(ctx, func(q Querier) error {
		if err := q.QueryRow(ctx, `SELECT 1 FROM scripts WHERE id = $1 FOR UPDATE`, id).Scan(new(int)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := q.QueryRow(ctx,
			`INSERT INTO script_versions (script_id, version, content, created_by, created_by_email, created_at)
			 VALUES ($1, (SELECT COALESCE(max(version), 0) + 1 FROM script_versions WHERE script_id = $1), $2, $3, $4, $5) RETURNING version`,
			id, content, by, byEmail, at).Scan(&v); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE scripts SET updated_at = $2 WHERE id = $1`, id, at)
		return err
	})
	return v, err
}

func (s *Store) ListScriptVersions(ctx context.Context, id uuid.UUID) ([]ScriptVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT version, '', created_by_email, created_at FROM script_versions WHERE script_id = $1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptVersion{}
	for rows.Next() {
		var v ScriptVersion
		if err := rows.Scan(&v.Version, &v.Content, &v.CreatedByEmail, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetScriptVersion devuelve una versión; 0 = la vigente (la más alta).
func (s *Store) GetScriptVersion(ctx context.Context, id uuid.UUID, version int) (ScriptVersion, error) {
	var v ScriptVersion
	err := s.pool.QueryRow(ctx,
		`SELECT version, content, created_by_email, created_at FROM script_versions
		 WHERE script_id = $1 AND ($2 = 0 OR version = $2) ORDER BY version DESC LIMIT 1`, id, version).
		Scan(&v.Version, &v.Content, &v.CreatedByEmail, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func (s *Store) ArchiveScript(ctx context.Context, id uuid.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE scripts SET archived_at = COALESCE(archived_at, $2) WHERE id = $1`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
