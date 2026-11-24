package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AssetDocument struct {
	ID            uuid.UUID
	AssetID       uuid.UUID
	SHA256        string
	Filename      string
	ContentType   string
	Size          int64
	UploadedBy    *uuid.UUID
	UploaderEmail string
	UploadedAt    time.Time
}

func (s *Store) AddAssetDocument(ctx context.Context, d AssetDocument) (AssetDocument, error) {
	err := s.pool.QueryRow(ctx, `INSERT INTO asset_documents (asset_id, sha256, filename, content_type, size, uploaded_by, uploaded_at)
	    VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		d.AssetID, d.SHA256, d.Filename, d.ContentType, d.Size, d.UploadedBy, d.UploadedAt).Scan(&d.ID)
	return d, err
}

const docCols = `d.id, d.asset_id, d.sha256, d.filename, d.content_type, d.size, d.uploaded_by, COALESCE(ad.email, ''), d.uploaded_at`

func docDest(d *AssetDocument) []any {
	return []any{&d.ID, &d.AssetID, &d.SHA256, &d.Filename, &d.ContentType, &d.Size, &d.UploadedBy, &d.UploaderEmail, &d.UploadedAt}
}

func (s *Store) ListAssetDocuments(ctx context.Context, assetID uuid.UUID) ([]AssetDocument, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+docCols+` FROM asset_documents d LEFT JOIN admins ad ON ad.id = d.uploaded_by
	    WHERE d.asset_id = $1 ORDER BY d.uploaded_at DESC, d.id`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetDocument{}
	for rows.Next() {
		var d AssetDocument
		if err := rows.Scan(docDest(&d)...); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AssetDocument(ctx context.Context, assetID, id uuid.UUID) (AssetDocument, error) {
	var d AssetDocument
	err := s.pool.QueryRow(ctx, `SELECT `+docCols+` FROM asset_documents d LEFT JOIN admins ad ON ad.id = d.uploaded_by
	    WHERE d.asset_id = $1 AND d.id = $2`, assetID, id).Scan(docDest(&d)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return AssetDocument{}, ErrNotFound
	}
	return d, err
}

func (s *Store) DeleteAssetDocument(ctx context.Context, assetID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM asset_documents WHERE asset_id = $1 AND id = $2`, assetID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func init() {
	unreferencedFilesQuery = `SELECT sha256 FROM files f WHERE uploaded_at < $1
		AND NOT EXISTS (SELECT 1 FROM app_versions v WHERE v.file_sha256 = f.sha256)
		AND NOT EXISTS (SELECT 1 FROM asset_documents d WHERE d.sha256 = f.sha256)`
}
