package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Estados de un activo.
const (
	AssetInUse    = "en_uso"
	AssetInStock  = "en_almacen"
	AssetInRepair = "en_reparacion"
	AssetRetired  = "baja"
)

// ValidAssetStatus informa de si st es un estado de activo.
func ValidAssetStatus(st string) bool {
	switch st {
	case AssetInUse, AssetInStock, AssetInRepair, AssetRetired:
		return true
	}
	return false
}

var (
	// ErrSerialTaken indica que otro activo ya tiene ese número de serie.
	ErrSerialTaken = errors.New("número de serie ocupado")
	// ErrTagTaken indica que otro activo ya tiene esa etiqueta.
	ErrTagTaken = errors.New("etiqueta ocupada")
	// ErrInvalid indica un valor no admitido.
	ErrInvalid = errors.New("valor no válido")
)

type Category struct {
	ID   uuid.UUID
	Name string
	Sort int
	// Assets es el número de activos (fuera de la papelera) de la categoría.
	Assets int
}

type Asset struct {
	ID            uuid.UUID
	DeviceID      *uuid.UUID
	CategoryID    uuid.UUID
	CategoryName  string
	CategoryAuto  bool
	Name          string
	Brand         string
	Model         string
	Serial        string
	AssetTag      string
	Status        string
	OwnerID       *uuid.UUID
	Department    string
	Location      string
	Price         *float64
	PurchaseDate  *time.Time
	WarrantyUntil *time.Time
	PurchaseOrder string
	Supplier      string
	Notes         string
	DeletedAt     *time.Time
	DeletedBy     *uuid.UUID
	DeletedReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AssetInput son los campos que un usuario fija en un activo.
type AssetInput struct {
	CategoryID    uuid.UUID
	Name          string
	Brand         string
	Model         string
	Serial        string
	AssetTag      string
	Status        string
	OwnerID       *uuid.UUID
	Department    string
	Location      string
	Price         *float64
	PurchaseDate  *time.Time
	WarrantyUntil *time.Time
	PurchaseOrder string
	Supplier      string
	Notes         string
}

func (in *AssetInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Serial = strings.TrimSpace(in.Serial)
	in.AssetTag = strings.TrimSpace(in.AssetTag)
	if in.Name == "" || !ValidAssetStatus(in.Status) || (in.Price != nil && *in.Price < 0) {
		return ErrInvalid
	}
	return nil
}

const assetCols = `a.id, a.device_id, a.category_id, c.name, a.category_auto, a.name, a.brand, a.model, a.serial,
	a.asset_tag, a.status, a.owner_id, a.department, a.location, a.price::float8, a.purchase_date, a.warranty_until,
	a.purchase_order, a.supplier, a.notes, a.deleted_at, a.deleted_by, a.deleted_reason, a.created_at, a.updated_at`

const assetFrom = ` FROM assets a JOIN asset_categories c ON c.id = a.category_id`

func assetDest(a *Asset) []any {
	return []any{&a.ID, &a.DeviceID, &a.CategoryID, &a.CategoryName, &a.CategoryAuto, &a.Name, &a.Brand, &a.Model, &a.Serial,
		&a.AssetTag, &a.Status, &a.OwnerID, &a.Department, &a.Location, &a.Price, &a.PurchaseDate, &a.WarrantyUntil,
		&a.PurchaseOrder, &a.Supplier, &a.Notes, &a.DeletedAt, &a.DeletedBy, &a.DeletedReason, &a.CreatedAt, &a.UpdatedAt}
}

// assetUniqueErr traduce las violaciones de unicidad de los activos.
func assetUniqueErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "23505" && pg.ConstraintName == "assets_serial_key_idx":
			return ErrSerialTaken
		case pg.Code == "23505" && pg.ConstraintName == "assets_tag_idx":
			return ErrTagTaken
		case pg.Code == "23503":
			return ErrInvalid
		}
	}
	return err
}

// querier es lo común a una transacción y al pool.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func assetByID(ctx context.Context, q querier, id uuid.UUID) (Asset, error) {
	var a Asset
	err := q.QueryRow(ctx, `SELECT `+assetCols+assetFrom+` WHERE a.id = $1`, id).Scan(assetDest(&a)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

func (s *Store) AssetByID(ctx context.Context, id uuid.UUID) (Asset, error) {
	return assetByID(ctx, s.pool, id)
}

// AssetByDevice devuelve el activo enlazado a un equipo.
func (s *Store) AssetByDevice(ctx context.Context, deviceID uuid.UUID) (Asset, error) {
	var a Asset
	err := s.pool.QueryRow(ctx, `SELECT `+assetCols+assetFrom+` WHERE a.device_id = $1`, deviceID).Scan(assetDest(&a)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

// CreateAsset da de alta un activo manual. Si su número de serie coincide con
// un equipo con agente que aún no tiene activo, quedan enlazados.
func (s *Store) CreateAsset(ctx context.Context, in AssetInput) (Asset, error) {
	if err := in.normalize(); err != nil {
		return Asset{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	var id uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO assets (category_id, name, brand, model, serial, serial_key, asset_tag, status, owner_id, department,
		                     location, price, purchase_date, warranty_until, purchase_order, supplier, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING id`,
		in.CategoryID, in.Name, in.Brand, in.Model, in.Serial, SerialKey(in.Serial), in.AssetTag, in.Status, in.OwnerID,
		in.Department, in.Location, in.Price, in.PurchaseDate, in.WarrantyUntil, in.PurchaseOrder, in.Supplier, in.Notes).Scan(&id)
	if err != nil {
		return Asset{}, assetUniqueErr(err)
	}
	if err := afterAssetWrite(ctx, tx, id); err != nil {
		return Asset{}, err
	}
	a, err := assetByID(ctx, tx, id)
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}

// UpdateAsset sustituye los campos editables. Cambiar la categoría deja de
// clasificarla automáticamente.
func (s *Store) UpdateAsset(ctx context.Context, id uuid.UUID, in AssetInput) (Asset, error) {
	if err := in.normalize(); err != nil {
		return Asset{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`UPDATE assets SET category_auto = category_auto AND category_id = $2, category_id = $2, name = $3, brand = $4,
		     model = $5, serial = $6, serial_key = $7, asset_tag = $8, status = $9, owner_id = $10, department = $11,
		     location = $12, price = $13, purchase_date = $14, warranty_until = $15, purchase_order = $16, supplier = $17,
		     notes = $18, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.CategoryID, in.Name, in.Brand, in.Model, in.Serial, SerialKey(in.Serial), in.AssetTag, in.Status, in.OwnerID,
		in.Department, in.Location, in.Price, in.PurchaseDate, in.WarrantyUntil, in.PurchaseOrder, in.Supplier, in.Notes)
	if err != nil {
		return Asset{}, assetUniqueErr(err)
	}
	if tag.RowsAffected() == 0 {
		return Asset{}, ErrNotFound
	}
	if err := afterAssetWrite(ctx, tx, id); err != nil {
		return Asset{}, err
	}
	a, err := assetByID(ctx, tx, id)
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}

// afterAssetWrite mantiene lo que depende de un activo tras guardarlo: el
// enlace con su equipo y el usuario asignado del equipo (Task 2).
var afterAssetWrite = func(ctx context.Context, q querier, id uuid.UUID) error { return nil }

// --- Categorías ---

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, c.sort,
	        (SELECT count(*) FROM assets a WHERE a.category_id = c.id AND a.deleted_at IS NULL)
	    FROM asset_categories c ORDER BY c.sort, lower(c.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Sort, &c.Assets); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CategoryByName(ctx context.Context, name string) (Category, error) {
	var c Category
	err := s.pool.QueryRow(ctx, `SELECT id, name, sort FROM asset_categories WHERE lower(name) = lower($1)`,
		strings.TrimSpace(name)).Scan(&c.ID, &c.Name, &c.Sort)
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateCategory(ctx context.Context, name string) (Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Category{}, ErrInvalid
	}
	c := Category{Name: name}
	err := s.pool.QueryRow(ctx, `INSERT INTO asset_categories (name, sort)
	    VALUES ($1, (SELECT COALESCE(max(sort), 0) + 10 FROM asset_categories)) RETURNING id, sort`, name).Scan(&c.ID, &c.Sort)
	if isUniqueViolation(err) {
		return Category{}, ErrConflict
	}
	return c, err
}

func (s *Store) RenameCategory(ctx context.Context, id uuid.UUID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE asset_categories SET name = $2 WHERE id = $1`, id, name)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteCategory borra una categoría que ningún activo usa (ni en la papelera).
func (s *Store) DeleteCategory(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM asset_categories WHERE id = $1`, id)
	if isForeignKeyViolation(err) {
		return ErrConflict
	}
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// --- Listado ---

type AssetFilter struct {
	Q          string
	Status     string
	Origin     string // agent | manual
	Department string
	CategoryID *uuid.UUID
	OwnerID    *uuid.UUID
	// Device filtra por datos del equipo; si tiene algo, solo salen activos con agente.
	Device DeviceFilter
	// Trash lista la papelera en lugar de los activos vivos.
	Trash   bool
	Sort    string
	Order   string
	Page    int
	PerPage int
	All     bool
}

type AssetRow struct {
	Asset
	OwnerName  string
	OwnerEmail string
	// OwnerDepartment es el departamento de la persona en Entra ID.
	OwnerDepartment string
	Device          *DeviceRow
}

var assetSorts = map[string]string{
	"name":       "lower(a.name)",
	"category":   "lower(c.name)",
	"status":     "a.status",
	"owner":      "lower(u.display_name)",
	"updated_at": "a.updated_at",
	"created_at": "a.created_at",
	"deleted_at": "a.deleted_at",
}

// ValidAssetSort informa de si sort es un criterio de ordenación admitido.
func ValidAssetSort(sort string) bool {
	_, ok := assetSorts[sort]
	return ok
}

func (f DeviceFilter) empty() bool {
	return f.Platform == "" && f.OSVersion == "" && f.Tag == "" && f.Status == "" && f.Compliance == "" && f.Issue == "" && f.GroupID == nil
}

func (f AssetFilter) where() (string, []any) {
	dev := f.Device
	dev.Q = ""
	devWhere, args := dev.where()
	conds := []string{}
	if devWhere != "" {
		conds = append(conds, strings.TrimPrefix(devWhere, " WHERE "))
	}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Trash {
		conds = append(conds, "a.deleted_at IS NOT NULL")
	} else {
		conds = append(conds, "a.deleted_at IS NULL")
	}
	if !dev.empty() || f.Origin == "agent" {
		conds = append(conds, "a.device_id IS NOT NULL")
	} else if f.Origin == "manual" {
		conds = append(conds, "a.device_id IS NULL")
	}
	if f.Q != "" {
		p := arg("%" + escapeLike(f.Q) + "%")
		conds = append(conds, fmt.Sprintf(`(a.name ILIKE %[1]s OR a.serial ILIKE %[1]s OR a.brand ILIKE %[1]s OR a.model ILIKE %[1]s
		    OR a.asset_tag ILIKE %[1]s OR a.location ILIKE %[1]s OR u.display_name ILIKE %[1]s OR u.email ILIKE %[1]s
		    OR COALESCE(d.hostname, '') ILIKE %[1]s)`, p))
	}
	if f.Status != "" {
		conds = append(conds, "a.status = "+arg(f.Status))
	}
	if f.Department != "" {
		conds = append(conds, "a.department = "+arg(f.Department))
	}
	if f.CategoryID != nil {
		conds = append(conds, "a.category_id = "+arg(*f.CategoryID))
	}
	if f.OwnerID != nil {
		conds = append(conds, "a.owner_id = "+arg(*f.OwnerID))
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const assetListFrom = assetFrom + ` LEFT JOIN devices d ON d.id = a.device_id LEFT JOIN directory_users u ON u.id = a.owner_id`

// ListAssets devuelve una página del inventario (activos con y sin agente).
func (s *Store) ListAssets(ctx context.Context, f AssetFilter) ([]AssetRow, int, error) {
	if f.Device.Now.IsZero() {
		f.Device.Now = time.Now()
	}
	where, args := f.where()
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+assetListFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar activos: %w", err)
	}
	sortExpr, ok := assetSorts[f.Sort]
	if !ok {
		sortExpr = assetSorts["name"]
	}
	dir := "ASC"
	if strings.EqualFold(f.Order, "desc") {
		dir = "DESC"
	}
	q := `SELECT ` + assetCols + `, COALESCE(u.display_name, ''), COALESCE(NULLIF(u.email, ''), u.upn, ''), COALESCE(u.department, '')` + assetListFrom + where +
		fmt.Sprintf(" ORDER BY %s %s NULLS LAST, a.id", sortExpr, dir)
	if !f.All {
		page, perPage := normalizePage(f.Page, f.PerPage)
		args = append(args, perPage, (page-1)*perPage)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar activos: %w", err)
	}
	out := []AssetRow{}
	var devIDs []uuid.UUID
	for rows.Next() {
		var r AssetRow
		if err := rows.Scan(append(assetDest(&r.Asset), &r.OwnerName, &r.OwnerEmail, &r.OwnerDepartment)...); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if r.DeviceID != nil {
			devIDs = append(devIDs, *r.DeviceID)
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(devIDs) > 0 {
		devs, err := s.deviceRowsByIDs(ctx, devIDs)
		if err != nil {
			return nil, 0, err
		}
		for i := range out {
			if out[i].DeviceID != nil {
				if d, ok := devs[*out[i].DeviceID]; ok {
					out[i].Device = &d
				}
			}
		}
	}
	return out, total, nil
}

func (s *Store) deviceRowsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]DeviceRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deviceCols+`, COALESCE(g.name, ''), COALESCE(c.state, 'unknown')
	      FROM devices d LEFT JOIN groups g ON g.id = d.group_id
	      LEFT JOIN device_compliance c ON c.device_id = d.id WHERE d.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]DeviceRow{}
	for rows.Next() {
		var r DeviceRow
		if err := rows.Scan(append(deviceDest(&r.Device), &r.GroupName, &r.Compliance)...); err != nil {
			return nil, err
		}
		out[r.ID] = r
	}
	return out, rows.Err()
}
