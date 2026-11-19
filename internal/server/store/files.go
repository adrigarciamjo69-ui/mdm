package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type File struct {
	SHA256       string
	Size         int64
	OriginalName string
	ContentType  string
	UploadedBy   *uuid.UUID
	UploadedAt   time.Time
}

// SaveFile registra un fichero subido. Si ya existía, conserva el registro original.
func (s *Store) SaveFile(ctx context.Context, f File) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO files (sha256, size, original_name, content_type, uploaded_by, uploaded_at) VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (sha256) DO NOTHING`, f.SHA256, f.Size, f.OriginalName, f.ContentType, f.UploadedBy, f.UploadedAt)
	return err
}

func (s *Store) GetFile(ctx context.Context, sha string) (File, error) {
	var f File
	err := s.pool.QueryRow(ctx, `SELECT sha256, size, original_name, content_type, uploaded_by, uploaded_at FROM files WHERE sha256 = $1`, sha).
		Scan(&f.SHA256, &f.Size, &f.OriginalName, &f.ContentType, &f.UploadedBy, &f.UploadedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) CountFiles(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM files`).Scan(&n)
	return n, err
}

// unreferencedFilesQuery lista los ficheros subidos antes de $1 que nada usa.
// La tarea del catálogo de aplicaciones añade la comprobación de referencias.
var unreferencedFilesQuery = `SELECT sha256 FROM files WHERE uploaded_at < $1`

// UnreferencedFiles devuelve los ficheros sin uso subidos antes de before.
func (s *Store) UnreferencedFiles(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, unreferencedFilesQuery, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFile(ctx context.Context, sha string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM files WHERE sha256 = $1`, sha)
	return err
}
