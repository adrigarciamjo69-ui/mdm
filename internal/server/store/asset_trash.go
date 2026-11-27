package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TrashAsset manda un activo a la papelera. ErrConflict si tiene un equipo
// enlazado (antes hay que eliminar el equipo o desenlazarlo).
func (s *Store) TrashAsset(ctx context.Context, id uuid.UUID, by *uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var linked bool
	err = tx.QueryRow(ctx, `SELECT device_id IS NOT NULL FROM assets WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&linked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if linked {
		return ErrConflict
	}
	if err := trashAsset(ctx, tx, id, by, CleanString(reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RestoreAsset saca un activo de la papelera. ErrSerialTaken o ErrTagTaken
// si mientras tanto otro activo ha tomado su número de serie o su etiqueta.
func (s *Store) RestoreAsset(ctx context.Context, id uuid.UUID) (Asset, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE assets SET deleted_at = NULL, deleted_by = NULL, deleted_reason = '', updated_at = now()
	    WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return Asset{}, assetUniqueErr(err)
	}
	if tag.RowsAffected() == 0 {
		return Asset{}, ErrNotFound
	}
	a, err := assetByID(ctx, tx, id)
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}

// PurgeAsset borra definitivamente un activo de la papelera, con sus
// documentos y asignaciones. ErrConflict si no está en la papelera.
func (s *Store) PurgeAsset(ctx context.Context, id uuid.UUID) error {
	var trashed bool
	err := s.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM assets WHERE id = $1`, id).Scan(&trashed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !trashed {
		return ErrConflict
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM assets WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	return err
}

// AssetBulk son los cambios de una edición masiva; nil = no tocar.
type AssetBulk struct {
	Status     *string
	CategoryID *uuid.UUID
	// OwnerID: puntero a nil quita el dueño.
	OwnerID  **uuid.UUID
	Location *string
	// GroupID cambia el grupo estático del equipo enlazado (puntero a nil lo
	// quita). Los activos sin equipo no se ven afectados.
	GroupID       **uuid.UUID
	PurchaseOrder *string
	Supplier      *string
	// Price: puntero a nil lo borra.
	Price **float64
}

// BulkUpdateAssets aplica los cambios a los activos vivos indicados y
// devuelve cuántos ha cambiado.
func (s *Store) BulkUpdateAssets(ctx context.Context, ids []uuid.UUID, b AssetBulk) (int, error) {
	if b.Status != nil && !ValidAssetStatus(*b.Status) {
		return 0, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	setOwner := b.OwnerID != nil
	var owner *uuid.UUID
	if setOwner {
		owner = *b.OwnerID
	}
	setPrice := b.Price != nil
	var price *float64
	if setPrice {
		price = *b.Price
		if price != nil && *price < 0 {
			return 0, ErrInvalid
		}
	}
	// Elegir la persona a mano la bloquea (si no, la siguiente sesión del
	// equipo la cambiaría); quitarla vuelve a dejar que siga a la sesión.
	tag, err := tx.Exec(ctx, `UPDATE assets SET
	        status = COALESCE($2, status),
	        category_auto = category_auto AND $3::uuid IS NULL, category_id = COALESCE($3, category_id),
	        owner_id = CASE WHEN $4 THEN $5::uuid ELSE owner_id END,
	        owner_locked = CASE WHEN $4 THEN $5::uuid IS NOT NULL ELSE owner_locked END,
	        location = COALESCE($6, location),
	        purchase_order = COALESCE($7, purchase_order), supplier = COALESCE($8, supplier),
	        price = CASE WHEN $9 THEN $10::numeric ELSE price END,
	        updated_at = now()
	    WHERE id = ANY($1) AND deleted_at IS NULL`, ids, b.Status, b.CategoryID, setOwner, owner, b.Location,
		b.PurchaseOrder, b.Supplier, setPrice, price)
	if err != nil {
		return 0, assetUniqueErr(err)
	}
	if b.GroupID != nil {
		rows, err := tx.Query(ctx, `UPDATE devices d SET group_id = $2 FROM assets a
		    WHERE a.id = ANY($1) AND a.deleted_at IS NULL AND d.id = a.device_id RETURNING d.id`, ids, *b.GroupID)
		if err != nil {
			if isForeignKeyViolation(err) {
				return 0, ErrBadReference
			}
			return 0, err
		}
		var devs []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			devs = append(devs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
		for _, d := range devs {
			if err := EnqueueRecomputeDevice(ctx, tx, d, time.Now()); err != nil {
				return 0, err
			}
		}
	}
	for _, id := range ids {
		if err := syncDeviceUser(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	return int(tag.RowsAffected()), tx.Commit(ctx)
}

// CreateAssets da de alta varios activos en una transacción (importación).
func (s *Store) CreateAssets(ctx context.Context, ins []AssetInput) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	for i := range ins {
		in := ins[i]
		if err := in.normalize(); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets (category_id, name, brand, model, serial, serial_key, asset_tag, status, owner_id, department,
			                     location, price, purchase_date, warranty_until, purchase_order, supplier, notes)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
			in.CategoryID, in.Name, in.Brand, in.Model, in.Serial, SerialKey(in.Serial), in.AssetTag, in.Status, in.OwnerID,
			in.Department, in.Location, in.Price, in.PurchaseDate, in.WarrantyUntil, in.PurchaseOrder, in.Supplier, in.Notes); err != nil {
			return 0, assetUniqueErr(err)
		}
	}
	return len(ins), tx.Commit(ctx)
}

// SerialKeyInUse informa de si algún activo vivo tiene ese número de serie
// normalizado (para validar una importación antes de guardarla).
func (s *Store) SerialKeyInUse(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM assets WHERE serial_key = $1 AND deleted_at IS NULL)`, key).Scan(&ok)
	return ok, err
}

// AssetTagInUse informa de si algún activo vivo tiene esa etiqueta.
func (s *Store) AssetTagInUse(ctx context.Context, tag string) (bool, error) {
	if tag == "" {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM assets WHERE lower(asset_tag) = lower($1) AND deleted_at IS NULL)`, tag).Scan(&ok)
	return ok, err
}
