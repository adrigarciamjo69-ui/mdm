package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// MergeFields son los campos que se eligen al fusionar dos activos.
var MergeFields = []string{"category", "name", "brand", "model", "serial", "asset_tag", "status", "owner", "department",
	"location", "price", "purchase_date", "warranty_until", "purchase_order", "supplier", "notes"}

// AssetMerge une dos fichas del mismo objeto. Se conserva KeepID; de OtherID
// se toman los campos marcados en FromOther. Con NotesBoth se juntan las
// notas de los dos. El equipo con agente, si lo tiene alguno, pasa al
// activo conservado.
type AssetMerge struct {
	KeepID, OtherID uuid.UUID
	FromOther       map[string]bool
	NotesBoth       bool
	By              *uuid.UUID
}

// MergeAssets fusiona dos activos: los documentos, las licencias y las actas
// del otro pasan al conservado, y el otro va a la papelera. ErrConflict si
// los dos tienen equipo con agente (son dos equipos distintos para el MDM);
// ErrNotFound si alguno no existe o está en la papelera.
func (s *Store) MergeAssets(ctx context.Context, m AssetMerge) (Asset, error) {
	if m.KeepID == m.OtherID {
		return Asset{}, ErrInvalid
	}
	known := map[string]bool{}
	for _, f := range MergeFields {
		known[f] = true
	}
	for f := range m.FromOther {
		if !known[f] {
			return Asset{}, ErrInvalid
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM assets WHERE id = ANY($1) AND deleted_at IS NULL FOR UPDATE) x`,
		[]uuid.UUID{m.KeepID, m.OtherID}).Scan(&n); err != nil {
		return Asset{}, err
	}
	if n != 2 {
		return Asset{}, ErrNotFound
	}
	keep, err := assetByID(ctx, tx, m.KeepID)
	if err != nil {
		return Asset{}, err
	}
	other, err := assetByID(ctx, tx, m.OtherID)
	if err != nil {
		return Asset{}, err
	}
	if keep.DeviceID != nil && other.DeviceID != nil {
		return Asset{}, ErrConflict
	}

	pick := func(f string) bool { return m.FromOther[f] }
	in := AssetInput{CategoryID: keep.CategoryID, Name: keep.Name, Brand: keep.Brand, Model: keep.Model, Serial: keep.Serial,
		AssetTag: keep.AssetTag, Status: keep.Status, OwnerID: keep.OwnerID, Department: keep.Department, Location: keep.Location,
		Price: keep.Price, PurchaseDate: keep.PurchaseDate, WarrantyUntil: keep.WarrantyUntil, PurchaseOrder: keep.PurchaseOrder,
		Supplier: keep.Supplier, Notes: keep.Notes}
	categoryAuto := keep.CategoryAuto
	if pick("category") {
		in.CategoryID, categoryAuto = other.CategoryID, other.CategoryAuto
	}
	str := map[string]*string{"name": &in.Name, "brand": &in.Brand, "model": &in.Model, "serial": &in.Serial, "asset_tag": &in.AssetTag,
		"status": &in.Status, "department": &in.Department, "location": &in.Location, "purchase_order": &in.PurchaseOrder,
		"supplier": &in.Supplier, "notes": &in.Notes}
	otherStr := map[string]string{"name": other.Name, "brand": other.Brand, "model": other.Model, "serial": other.Serial,
		"asset_tag": other.AssetTag, "status": other.Status, "department": other.Department, "location": other.Location,
		"purchase_order": other.PurchaseOrder, "supplier": other.Supplier, "notes": other.Notes}
	for f, dst := range str {
		if pick(f) {
			*dst = otherStr[f]
		}
	}
	if pick("owner") {
		in.OwnerID = other.OwnerID
	}
	if pick("price") {
		in.Price = other.Price
	}
	if pick("purchase_date") {
		in.PurchaseDate = other.PurchaseDate
	}
	if pick("warranty_until") {
		in.WarrantyUntil = other.WarrantyUntil
	}
	if m.NotesBoth {
		var parts []string
		for _, n := range []string{keep.Notes, other.Notes} {
			if n = strings.TrimSpace(n); n != "" && !contains(parts, n) {
				parts = append(parts, n)
			}
		}
		in.Notes = strings.Join(parts, "\n\n")
	}
	if err := in.normalize(); err != nil {
		return Asset{}, err
	}
	device := keep.DeviceID
	if device == nil {
		device = other.DeviceID
	}

	// Lo que cuelga del otro pasa al conservado (sin duplicar licencias).
	for _, q := range []string{
		`UPDATE asset_documents SET asset_id = $1 WHERE asset_id = $2`,
		`DELETE FROM license_assignments o WHERE o.asset_id = $2
		     AND EXISTS (SELECT 1 FROM license_assignments k WHERE k.asset_id = $1 AND k.license_id = o.license_id)`,
		`UPDATE license_assignments SET asset_id = $1 WHERE asset_id = $2`,
		`UPDATE delivery_items SET asset_id = $1 WHERE asset_id = $2`,
	} {
		if _, err := tx.Exec(ctx, q, m.KeepID, m.OtherID); err != nil {
			return Asset{}, err
		}
	}
	name := in.Name
	if err := trashAsset(ctx, tx, m.OtherID, m.By, fmt.Sprintf("Fusionado con «%s»", name)); err != nil {
		return Asset{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE assets SET category_id = $2, category_auto = $3, name = $4, brand = $5, model = $6, serial = $7, serial_key = $8,
		     asset_tag = $9, status = $10, owner_id = $11, department = $12, location = $13, price = $14, purchase_date = $15,
		     warranty_until = $16, purchase_order = $17, supplier = $18, notes = $19, device_id = $20, updated_at = now()
		 WHERE id = $1`,
		m.KeepID, in.CategoryID, categoryAuto, in.Name, in.Brand, in.Model, in.Serial, SerialKey(in.Serial), in.AssetTag, in.Status,
		in.OwnerID, in.Department, in.Location, in.Price, in.PurchaseDate, in.WarrantyUntil, in.PurchaseOrder, in.Supplier, in.Notes,
		device); err != nil {
		return Asset{}, assetUniqueErr(err)
	}
	if err := afterAssetWrite(ctx, tx, m.KeepID); err != nil {
		return Asset{}, err
	}
	a, err := assetByID(ctx, tx, m.KeepID)
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
