package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

func init() { afterAssetWrite = syncDeviceUser }

// syncDeviceUser mantiene lo que depende del dueño del activo: si es una
// persona de Entra ID con departamento, el activo toma ese departamento (salvo
// que esté bloqueado); y el
// equipo enlazado copia su correo y recalcula sus grupos dinámicos (las reglas
// por usuario y por departamento).
func syncDeviceUser(ctx context.Context, q querier, assetID uuid.UUID) error {
	if _, err := q.Exec(ctx, `UPDATE assets a SET department = u.department
	    FROM directory_users u WHERE a.id = $1 AND u.id = a.owner_id AND u.department <> '' AND a.department <> u.department
	    AND NOT a.department_locked`, assetID); err != nil {
		return err
	}
	var deviceID *uuid.UUID
	if err := q.QueryRow(ctx, `UPDATE devices d SET assigned_user = COALESCE(NULLIF(u.email, ''), u.upn, '')
	    FROM assets a LEFT JOIN directory_users u ON u.id = a.owner_id
	    WHERE a.id = $1 AND d.id = a.device_id RETURNING d.id`, assetID).Scan(&deviceID); err != nil && err != pgx.ErrNoRows {
		return err
	}
	if deviceID != nil {
		return EnqueueRecomputeDevice(ctx, q, *deviceID, time.Now())
	}
	return nil
}

// sessionLogin separa el usuario con sesión que informa el agente
// ("DOMINIO\usuario", "usuario@dominio" o "usuario") en el nombre corto y,
// si lo hay, el UPN completo. Ambos en minúsculas.
func sessionLogin(login string) (short, upn string) {
	l := strings.ToLower(strings.TrimSpace(login))
	if i := strings.LastIndex(l, `\`); i >= 0 {
		l = l[i+1:]
	}
	if i := strings.Index(l, "@"); i >= 0 {
		return l[:i], l
	}
	return l, ""
}

// directoryUserForLogin busca la persona de Entra ID que corresponde al
// usuario con sesión en un equipo. Primero por coincidencia exacta (usuario
// de Windows de AD o UPN) y si no, por la parte del UPN o del correo antes de
// la arroba. Si no hay ninguna o hay dos igual de buenas, devuelve nil.
func directoryUserForLogin(ctx context.Context, q querier, login string) (*uuid.UUID, error) {
	short, upn := sessionLogin(login)
	if short == "" {
		return nil, nil
	}
	// $2 es NULL si la sesión no trae UPN: comparar con '' haría «exacta» a
	// cualquier persona sin correo.
	var full *string
	if upn != "" {
		full = &upn
	}
	rows, err := q.Query(ctx, `SELECT id, COALESCE(lower(sam_account) = $1 OR lower(upn) = $2 OR lower(email) = $2, false) AS exact, enabled
	    FROM (SELECT * FROM directory_users WHERE deleted_at IS NULL) u
	    WHERE (sam_account <> '' AND lower(sam_account) = $1) OR lower(upn) = $2 OR lower(email) = $2
	        OR (upn <> '' AND split_part(lower(upn), '@', 1) = $1) OR (email <> '' AND split_part(lower(email), '@', 1) = $1)
	    ORDER BY exact DESC, enabled DESC LIMIT 2`, short, full)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cand struct {
		id             uuid.UUID
		exact, enabled bool
	}
	var cs []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.exact, &c.enabled); err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch {
	case len(cs) == 0:
		return nil, nil
	case len(cs) == 2 && cs[0].exact == cs[1].exact && cs[0].enabled == cs[1].enabled:
		return nil, nil // ambiguo: mejor no tocar nada
	}
	return &cs[0].id, nil
}

// applySessionUser hace que la persona del activo de un equipo sea la del
// usuario con la sesión iniciada, salvo que la persona esté bloqueada. Si el
// usuario no corresponde a nadie de Entra ID (una cuenta local, por ejemplo)
// no cambia nada.
func applySessionUser(ctx context.Context, q querier, deviceID uuid.UUID, login string) error {
	owner, err := directoryUserForLogin(ctx, q, login)
	if err != nil || owner == nil {
		return err
	}
	var assetID uuid.UUID
	err = q.QueryRow(ctx, `UPDATE assets SET owner_id = $2, updated_at = now()
	    WHERE device_id = $1 AND deleted_at IS NULL AND NOT owner_locked AND owner_id IS DISTINCT FROM $2
	    RETURNING id`, deviceID, *owner).Scan(&assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDeviceUser(ctx, q, assetID)
}

// applyAllSessionUsers aplica applySessionUser a todos los equipos con el
// usuario con sesión de su último inventario guardado. Sirve para no esperar
// a que cada agente vuelva a enviar el inventario (tras una sincronización con
// Entra ID o al desplegar). Devuelve cuántos activos han cambiado de persona.
func applyAllSessionUsers(ctx context.Context, q querier) (int, error) {
	rows, err := q.Query(ctx, `SELECT a.device_id, a.owner_id, i.data->'system'->>'logged_in_user'
	    FROM assets a JOIN device_inventory i ON i.device_id = a.device_id
	    WHERE a.deleted_at IS NULL AND NOT a.owner_locked AND COALESCE(i.data->'system'->>'logged_in_user', '') <> ''`)
	if err != nil {
		return 0, err
	}
	type item struct {
		dev   uuid.UUID
		owner *uuid.UUID
		login string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.dev, &it.owner, &it.login); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		owner, err := directoryUserForLogin(ctx, q, it.login)
		if err != nil {
			return n, err
		}
		if owner == nil || (it.owner != nil && *it.owner == *owner) {
			continue
		}
		if err := applySessionUser(ctx, q, it.dev, it.login); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ApplyAllSessionUsers: ver applyAllSessionUsers.
func (s *Store) ApplyAllSessionUsers(ctx context.Context) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	n, err := applyAllSessionUsers(ctx, tx)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
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
