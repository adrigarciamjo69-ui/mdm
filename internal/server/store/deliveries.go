package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tipos y estados de un acta.
const (
	DeliveryEntrega    = "entrega"
	DeliveryDevolucion = "devolucion"
	DeliveryDraft      = "borrador"
	DeliveryConfirmed  = "confirmada"
	DeliveryCancelled  = "anulada"
)

type HandoverItem struct {
	ID           uuid.UUID
	AssetID      *uuid.UUID
	Category     string
	Brand        string
	Model        string
	Serial       string
	Observations string
}

type Handover struct {
	ID             uuid.UUID
	Number         string
	Type           string
	UserID         uuid.UUID
	User           DirectoryUser
	RecipientDNI   string
	Date           time.Time
	Responsible    string
	Status         string
	Notes          string
	CancelReason   string
	CreatedBy      *uuid.UUID
	CreatedByEmail string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ConfirmedAt    *time.Time
	Items          []HandoverItem
}

type HandoverInput struct {
	Type         string
	UserID       uuid.UUID
	RecipientDNI string
	Date         time.Time
	Responsible  string
	Notes        string
	CreatedBy    *uuid.UUID
	Items        []HandoverItem
}

// ErrAssetUnavailable indica que algún activo del acta no existe o está en la papelera.
var ErrAssetUnavailable = errors.New("activo no disponible")

// fillItems completa cada línea con la copia de los datos de su activo.
func fillItems(ctx context.Context, q querier, items []HandoverItem) error {
	for i := range items {
		it := &items[i]
		if it.AssetID == nil {
			continue
		}
		var cat, brand, model, serial string
		err := q.QueryRow(ctx, `SELECT c.name, a.brand, a.model, a.serial FROM assets a JOIN asset_categories c ON c.id = a.category_id
		    WHERE a.id = $1 AND a.deleted_at IS NULL`, *it.AssetID).Scan(&cat, &brand, &model, &serial)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAssetUnavailable
		}
		if err != nil {
			return err
		}
		it.Category, it.Brand, it.Model, it.Serial = cat, brand, model, serial
	}
	return nil
}

func writeItems(ctx context.Context, q querier, recordID uuid.UUID, items []HandoverItem) error {
	if _, err := q.Exec(ctx, `DELETE FROM delivery_items WHERE record_id = $1`, recordID); err != nil {
		return err
	}
	for i, it := range items {
		if _, err := q.Exec(ctx, `INSERT INTO delivery_items (record_id, position, asset_id, category, brand, model, serial, observations)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, recordID, i, it.AssetID, it.Category, it.Brand, it.Model, it.Serial, it.Observations); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateHandover(ctx context.Context, in HandoverInput) (Handover, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Handover{}, err
	}
	defer tx.Rollback(ctx)
	if err := fillItems(ctx, tx, in.Items); err != nil {
		return Handover{}, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO delivery_records (type, user_id, recipient_dni, date, responsible, notes, created_by)
	    VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.Type, in.UserID, in.RecipientDNI, in.Date, in.Responsible, in.Notes, in.CreatedBy).Scan(&id)
	if isForeignKeyViolation(err) {
		return Handover{}, ErrInvalid
	}
	if err != nil {
		return Handover{}, err
	}
	if err := writeItems(ctx, tx, id, in.Items); err != nil {
		return Handover{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handover{}, err
	}
	return s.HandoverByID(ctx, id)
}

// lockDraft bloquea el acta y comprueba su estado.
func lockHandover(ctx context.Context, q querier, id uuid.UUID) (string, error) {
	var st string
	err := q.QueryRow(ctx, `SELECT status FROM delivery_records WHERE id = $1 FOR UPDATE`, id).Scan(&st)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return st, err
}

// UpdateHandover sustituye un borrador. ErrConflict si ya no es borrador.
func (s *Store) UpdateHandover(ctx context.Context, id uuid.UUID, in HandoverInput) (Handover, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Handover{}, err
	}
	defer tx.Rollback(ctx)
	st, err := lockHandover(ctx, tx, id)
	if err != nil {
		return Handover{}, err
	}
	if st != DeliveryDraft {
		return Handover{}, ErrConflict
	}
	if err := fillItems(ctx, tx, in.Items); err != nil {
		return Handover{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE delivery_records SET type = $2, user_id = $3, recipient_dni = $4, date = $5, responsible = $6,
	    notes = $7, updated_at = now() WHERE id = $1`, id, in.Type, in.UserID, in.RecipientDNI, in.Date, in.Responsible, in.Notes)
	if isForeignKeyViolation(err) {
		return Handover{}, ErrInvalid
	}
	if err != nil {
		return Handover{}, err
	}
	if err := writeItems(ctx, tx, id, in.Items); err != nil {
		return Handover{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handover{}, err
	}
	return s.HandoverByID(ctx, id)
}

// DeleteHandover borra un borrador. ErrConflict si ya no es borrador.
func (s *Store) DeleteHandover(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	st, err := lockHandover(ctx, tx, id)
	if err != nil {
		return err
	}
	if st != DeliveryDraft {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM delivery_records WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConfirmHandover confirma un borrador: le da número definitivo y aplica sus
// efectos a los activos (entrega: pasan a la persona y en uso; devolución:
// sin dueño y en almacén). ErrConflict si no es borrador; ErrInvalid si no
// tiene líneas; ErrAssetUnavailable si algún activo ya no existe.
func (s *Store) ConfirmHandover(ctx context.Context, id uuid.UUID, at time.Time) (Handover, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Handover{}, err
	}
	defer tx.Rollback(ctx)
	st, err := lockHandover(ctx, tx, id)
	if err != nil {
		return Handover{}, err
	}
	if st != DeliveryDraft {
		return Handover{}, ErrConflict
	}
	var typ string
	var userID uuid.UUID
	var date time.Time
	if err := tx.QueryRow(ctx, `SELECT type, user_id, date FROM delivery_records WHERE id = $1`, id).Scan(&typ, &userID, &date); err != nil {
		return Handover{}, err
	}
	var items int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_items WHERE record_id = $1`, id).Scan(&items); err != nil {
		return Handover{}, err
	}
	if items == 0 {
		return Handover{}, ErrInvalid
	}
	var missing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_items i LEFT JOIN assets a ON a.id = i.asset_id
	    WHERE i.record_id = $1 AND i.asset_id IS NOT NULL AND (a.id IS NULL OR a.deleted_at IS NOT NULL)`, id).Scan(&missing); err != nil {
		return Handover{}, err
	}
	if missing > 0 {
		return Handover{}, ErrAssetUnavailable
	}
	var n int
	if err := tx.QueryRow(ctx, `INSERT INTO delivery_counters (type, year, last) VALUES ($1, $2, 1)
	    ON CONFLICT (type, year) DO UPDATE SET last = delivery_counters.last + 1 RETURNING last`, typ, date.Year()).Scan(&n); err != nil {
		return Handover{}, err
	}
	prefix := "ENT"
	if typ == DeliveryDevolucion {
		prefix = "DEV"
	}
	number := fmt.Sprintf("%s-%d-%04d", prefix, date.Year(), n)
	if _, err := tx.Exec(ctx, `UPDATE delivery_records SET status = 'confirmada', number = $2, confirmed_at = $3, updated_at = now()
	    WHERE id = $1`, id, number, at); err != nil {
		return Handover{}, err
	}
	if typ == DeliveryEntrega {
		_, err = tx.Exec(ctx, `UPDATE assets a SET owner_id = $2, status = 'en_uso',
		        department = CASE WHEN u.department <> '' THEN u.department ELSE a.department END, updated_at = now()
		    FROM directory_users u WHERE u.id = $2 AND a.id IN (SELECT asset_id FROM delivery_items WHERE record_id = $1 AND asset_id IS NOT NULL)`,
			id, userID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE assets SET owner_id = NULL, status = 'en_almacen', updated_at = now()
		    WHERE id IN (SELECT asset_id FROM delivery_items WHERE record_id = $1 AND asset_id IS NOT NULL)`, id)
	}
	if err != nil {
		return Handover{}, err
	}
	rows, err := tx.Query(ctx, `SELECT asset_id FROM delivery_items WHERE record_id = $1 AND asset_id IS NOT NULL`, id)
	if err != nil {
		return Handover{}, err
	}
	var assetIDs []uuid.UUID
	for rows.Next() {
		var a uuid.UUID
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return Handover{}, err
		}
		assetIDs = append(assetIDs, a)
	}
	rows.Close()
	for _, a := range assetIDs {
		if err := syncDeviceUser(ctx, tx, a); err != nil {
			return Handover{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Handover{}, err
	}
	return s.HandoverByID(ctx, id)
}

// CancelHandover anula un acta confirmada (no deshace sus efectos).
func (s *Store) CancelHandover(ctx context.Context, id uuid.UUID, reason string) (Handover, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE delivery_records SET status = 'anulada', cancel_reason = $2, updated_at = now()
	    WHERE id = $1 AND status = 'confirmada'`, id, reason)
	if err != nil {
		return Handover{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.HandoverByID(ctx, id); err != nil {
			return Handover{}, err
		}
		return Handover{}, ErrConflict
	}
	return s.HandoverByID(ctx, id)
}

const deliveryCols = `r.id, r.number, r.type, r.user_id, r.recipient_dni, r.date, r.responsible, r.status, r.notes, r.cancel_reason,
	r.created_by, COALESCE(ad.email, ''), r.created_at, r.updated_at, r.confirmed_at, ` + dirCols

const deliveryFrom = ` FROM delivery_records r JOIN directory_users u ON u.id = r.user_id LEFT JOIN admins ad ON ad.id = r.created_by`

func deliveryDest(d *Handover) []any {
	return append([]any{&d.ID, &d.Number, &d.Type, &d.UserID, &d.RecipientDNI, &d.Date, &d.Responsible, &d.Status, &d.Notes,
		&d.CancelReason, &d.CreatedBy, &d.CreatedByEmail, &d.CreatedAt, &d.UpdatedAt, &d.ConfirmedAt}, dirDest(&d.User)...)
}

func (s *Store) HandoverByID(ctx context.Context, id uuid.UUID) (Handover, error) {
	var d Handover
	err := s.pool.QueryRow(ctx, `SELECT `+deliveryCols+deliveryFrom+` WHERE r.id = $1`, id).Scan(deliveryDest(&d)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Handover{}, ErrNotFound
	}
	if err != nil {
		return Handover{}, err
	}
	m, err := s.handoverItems(ctx, []uuid.UUID{id})
	d.Items = m[id]
	if d.Items == nil {
		d.Items = []HandoverItem{}
	}
	return d, err
}

func (s *Store) handoverItems(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]HandoverItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT record_id, id, asset_id, category, brand, model, serial, observations
	    FROM delivery_items WHERE record_id = ANY($1) ORDER BY record_id, position`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]HandoverItem{}
	for rows.Next() {
		var rid uuid.UUID
		var it HandoverItem
		if err := rows.Scan(&rid, &it.ID, &it.AssetID, &it.Category, &it.Brand, &it.Model, &it.Serial, &it.Observations); err != nil {
			return nil, err
		}
		out[rid] = append(out[rid], it)
	}
	return out, rows.Err()
}

type HandoverFilter struct {
	Q       string
	Type    string
	Status  string
	UserID  *uuid.UUID
	AssetID *uuid.UUID
	Page    int
	PerPage int
	All     bool
}

func (s *Store) ListHandovers(ctx context.Context, f HandoverFilter) ([]Handover, int, error) {
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Q != "" {
		p := arg("%" + escapeLike(f.Q) + "%")
		conds = append(conds, fmt.Sprintf("(r.number ILIKE %[1]s OR u.display_name ILIKE %[1]s OR u.email ILIKE %[1]s)", p))
	}
	if f.Type != "" {
		conds = append(conds, "r.type = "+arg(f.Type))
	}
	if f.Status != "" {
		conds = append(conds, "r.status = "+arg(f.Status))
	}
	if f.UserID != nil {
		conds = append(conds, "r.user_id = "+arg(*f.UserID))
	}
	if f.AssetID != nil {
		conds = append(conds, "EXISTS (SELECT 1 FROM delivery_items i WHERE i.record_id = r.id AND i.asset_id = "+arg(*f.AssetID)+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+deliveryFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + deliveryCols + deliveryFrom + where + ` ORDER BY r.date DESC, r.created_at DESC, r.id`
	if !f.All {
		page, perPage := normalizePage(f.Page, f.PerPage)
		args = append(args, perPage, (page-1)*perPage)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	out := []Handover{}
	var ids []uuid.UUID
	for rows.Next() {
		var d Handover
		if err := rows.Scan(deliveryDest(&d)...); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, d)
		ids = append(ids, d.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	items, err := s.handoverItems(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Items = items[out[i].ID]
		if out[i].Items == nil {
			out[i].Items = []HandoverItem{}
		}
	}
	return out, total, nil
}

// DeliverySettingsRaw devuelve los ajustes de las actas guardados (nil si no hay).
func (s *Store) DeliverySettingsRaw(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT data FROM delivery_settings`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func (s *Store) SaveDeliverySettings(ctx context.Context, raw json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO delivery_settings (id, data) VALUES (true, $1) ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data`, raw)
	return err
}
