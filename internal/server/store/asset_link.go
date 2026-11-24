package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

func init() { afterAssetWrite = syncDeviceUser }

// syncDeviceUser copia al equipo enlazado el correo del dueño del activo,
// para que las reglas de grupos por usuario sigan funcionando.
func syncDeviceUser(ctx context.Context, q querier, assetID uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE devices d SET assigned_user = COALESCE(NULLIF(u.email, ''), u.upn, '')
	    FROM assets a LEFT JOIN directory_users u ON u.id = a.owner_id
	    WHERE a.id = $1 AND d.id = a.device_id`, assetID)
	return err
}

// categoryFor elige la categoría inicial de un equipo con agente.
func categoryFor(platform, osName, chassis string) string {
	switch {
	case platform == "android":
		return "Móvil"
	case chassis == protocol.ChassisServer || strings.Contains(strings.ToLower(osName), "server"):
		return "Servidor"
	case chassis == protocol.ChassisLaptop:
		return "Portátil"
	case chassis == protocol.ChassisDesktop:
		return "Sobremesa"
	}
	return "Ordenador"
}

func categoryIDByName(ctx context.Context, q querier, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM asset_categories WHERE lower(name) = lower($1)`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Alguien ha borrado la categoría: se usa la primera que haya.
		err = q.QueryRow(ctx, `SELECT id FROM asset_categories ORDER BY sort LIMIT 1`).Scan(&id)
	}
	return id, err
}

// ensureDeviceAsset garantiza que el equipo tiene un activo: si no, lo une
// por número de serie con un activo manual sin equipo o crea uno nuevo.
// Devuelve true si ha creado o enlazado uno.
func ensureDeviceAsset(ctx context.Context, q querier, deviceID uuid.UUID) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM assets WHERE device_id = $1)`, deviceID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	var platform, osName, hostname, serial, manufacturer, model string
	if err := q.QueryRow(ctx, `SELECT platform, os_name, hostname, serial, manufacturer, model FROM devices WHERE id = $1`, deviceID).
		Scan(&platform, &osName, &hostname, &serial, &manufacturer, &model); err != nil {
		return false, fmt.Errorf("leer equipo: %w", err)
	}
	key := SerialKey(serial)
	if key != "" {
		var assetID uuid.UUID
		var linked bool
		err := q.QueryRow(ctx, `SELECT id, device_id IS NOT NULL FROM assets
		    WHERE serial_key = $1 AND deleted_at IS NULL FOR UPDATE`, key).Scan(&assetID, &linked)
		switch {
		case err == nil && !linked:
			_, err := q.Exec(ctx, `UPDATE assets SET device_id = $2, brand = CASE WHEN brand = '' THEN $3 ELSE brand END,
			    model = CASE WHEN model = '' THEN $4 ELSE model END, updated_at = now() WHERE id = $1`,
				assetID, deviceID, manufacturer, model)
			if err != nil {
				return false, err
			}
			return true, syncDeviceUser(ctx, q, assetID)
		case err == nil && linked:
			// Otro equipo (por ejemplo, una reinstalación) ya tiene ese número:
			// el activo nuevo no participa en la unión.
			key = ""
		case !errors.Is(err, pgx.ErrNoRows):
			return false, err
		}
	}
	catID, err := categoryIDByName(ctx, q, categoryFor(platform, osName, ""))
	if err != nil {
		return false, err
	}
	name := hostname
	if name == "" {
		name = strings.TrimSpace(manufacturer + " " + model)
	}
	if name == "" {
		name = "Equipo sin nombre"
	}
	_, err = q.Exec(ctx, `INSERT INTO assets (device_id, category_id, category_auto, name, brand, model, serial, serial_key, status)
	    VALUES ($1, $2, true, $3, $4, $5, $6, $7, 'en_uso')`, deviceID, catID, name, manufacturer, model, serial, key)
	return err == nil, err
}

// applyChassis reclasifica el activo de un equipo con el chasis que informa
// el agente, solo si nadie ha elegido su categoría a mano.
func applyChassis(ctx context.Context, q querier, deviceID uuid.UUID, chassis string) error {
	if chassis == "" {
		return nil
	}
	var platform, osName string
	if err := q.QueryRow(ctx, `SELECT platform, os_name FROM devices WHERE id = $1`, deviceID).Scan(&platform, &osName); err != nil {
		return err
	}
	catID, err := categoryIDByName(ctx, q, categoryFor(platform, osName, chassis))
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE assets SET category_id = $2, updated_at = now()
	    WHERE device_id = $1 AND category_auto AND category_id <> $2`, deviceID, catID)
	return err
}

// EnsureAllDeviceAssets crea o enlaza el activo de cada equipo que no lo
// tenga (equipos anteriores al inventario o activos borrados).
func (s *Store) EnsureAllDeviceAssets(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id FROM devices d WHERE NOT EXISTS (SELECT 1 FROM assets a WHERE a.device_id = d.id)`)
	if err != nil {
		return 0, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return n, err
		}
		done, err := ensureDeviceAsset(ctx, tx, id)
		if err == nil {
			err = tx.Commit(ctx)
		}
		tx.Rollback(ctx)
		if err != nil {
			return n, err
		}
		if done {
			n++
		}
	}
	return n, nil
}

// trashAsset manda un activo a la papelera: pierde su dueño y su equipo.
func trashAsset(ctx context.Context, q querier, id uuid.UUID, by *uuid.UUID, reason string) error {
	tag, err := q.Exec(ctx, `UPDATE assets SET deleted_at = now(), deleted_by = $2, deleted_reason = $3, owner_id = NULL,
	    device_id = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id, by, reason)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LinkAsset enlaza un activo sin equipo con un equipo. El activo que tuviera
// el equipo va a la papelera. ErrConflict si el activo ya tiene equipo o
// está en la papelera.
func (s *Store) LinkAsset(ctx context.Context, assetID, deviceID uuid.UUID, by *uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var linked, trashed bool
	err = tx.QueryRow(ctx, `SELECT device_id IS NOT NULL, deleted_at IS NOT NULL FROM assets WHERE id = $1 FOR UPDATE`, assetID).Scan(&linked, &trashed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if linked || trashed {
		return ErrConflict
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1)`, deviceID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	var prev uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM assets WHERE device_id = $1`, deviceID).Scan(&prev)
	if err == nil {
		if err := trashAsset(ctx, tx, prev, by, "Sustituido al enlazar el equipo con otro activo"); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE assets SET device_id = $2, category_auto = false, updated_at = now() WHERE id = $1`, assetID, deviceID); err != nil {
		return err
	}
	if err := syncDeviceUser(ctx, tx, assetID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UnlinkAsset separa un activo de su equipo: el activo queda como manual
// (sin participar en la unión por número de serie) y el equipo recibe un
// activo nuevo. ErrConflict si el activo no tiene equipo.
func (s *Store) UnlinkAsset(ctx context.Context, assetID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deviceID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT device_id FROM assets WHERE id = $1 FOR UPDATE`, assetID).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if deviceID == nil {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE assets SET device_id = NULL, serial_key = '', updated_at = now() WHERE id = $1`, assetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET assigned_user = '' WHERE id = $1`, *deviceID); err != nil {
		return err
	}
	if _, err := ensureDeviceAsset(ctx, tx, *deviceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
