package store_test

import (
	"testing"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func TestAssets_MultiValueFilters(t *testing.T) {
	s := storetest.New(t)
	mon, lap := category(t, s, "Monitor"), category(t, s, "Portátil")
	newAsset(t, s, store.AssetInput{CategoryID: mon, Name: "M1", Status: store.AssetInUse, Department: "Taller"})
	newAsset(t, s, store.AssetInput{CategoryID: lap, Name: "L1", Status: store.AssetInStock, Department: "Finanzas"})
	newAsset(t, s, store.AssetInput{CategoryID: lap, Name: "L2", Status: store.AssetInUse, Department: "R-D"})
	newAsset(t, s, store.AssetInput{CategoryID: mon, Name: "M2", Status: store.AssetRetired})
	count := func(f store.AssetFilter) int {
		t.Helper()
		f.PerPage = 50
		_, n, err := s.ListAssets(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(store.AssetFilter{Departments: []string{"Taller", "Finanzas"}}); n != 2 {
		t.Fatalf("dos departamentos: %d", n)
	}
	if n := count(store.AssetFilter{Departments: []string{"Taller", ""}}); n != 2 {
		t.Fatalf("departamento o sin departamento: %d", n)
	}
	if n := count(store.AssetFilter{CategoryIDs: []uuid.UUID{mon, lap}, Statuses: []string{store.AssetInUse, store.AssetRetired}}); n != 3 {
		t.Fatalf("categorías y estados: %d", n)
	}
	if n := count(store.AssetFilter{CategoryIDs: []uuid.UUID{lap}, Departments: []string{"Taller"}}); n != 0 {
		t.Fatalf("los filtros distintos se combinan con Y: %d", n)
	}
}

func TestAssets_BulkGroupPriceOrder(t *testing.T) {
	s := storetest.New(t)
	d1 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "h1", Hostname: "PC1"})
	d2 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "h2", Hostname: "PC2"})
	manual := newAsset(t, s, store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "M1", Status: store.AssetInUse})
	g, err := s.CreateGroup(ctx, "Oficina Bilbao", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{assetOf(t, s, d1).ID, assetOf(t, s, d2).ID, manual.ID}
	gid, price, po := &g.ID, 899.5, "PO-2026-114"
	pp := &price
	n, err := s.BulkUpdateAssets(ctx, ids, store.AssetBulk{GroupID: &gid, Price: &pp, PurchaseOrder: &po})
	if err != nil || n != 3 {
		t.Fatalf("bulk: n=%d err=%v", n, err)
	}
	for _, d := range []uuid.UUID{d1, d2} {
		dev, _ := s.DeviceByID(ctx, d)
		if dev.GroupID == nil || *dev.GroupID != g.ID {
			t.Fatalf("grupo de %s: %v", d, dev.GroupID)
		}
	}
	rows, _, _ := s.ListAssets(ctx, store.AssetFilter{Device: store.DeviceFilter{GroupIDs: []uuid.UUID{g.ID}}, PerPage: 10})
	if len(rows) != 2 || len(rows[0].Groups) != 1 || rows[0].Groups[0].Name != "Oficina Bilbao" || rows[0].Groups[0].Dynamic {
		t.Fatalf("filtro y grupos: %+v", rows)
	}
	m, _ := s.AssetByID(ctx, manual.ID)
	if m.Price == nil || *m.Price != 899.5 || m.PurchaseOrder != "PO-2026-114" {
		t.Fatalf("precio y pedido: %+v", m)
	}
	// Quitar grupo y precio.
	var none *uuid.UUID
	var noPrice *float64
	if _, err := s.BulkUpdateAssets(ctx, ids[:1], store.AssetBulk{GroupID: &none, Price: &noPrice}); err != nil {
		t.Fatal(err)
	}
	if dev, _ := s.DeviceByID(ctx, d1); dev.GroupID != nil {
		t.Fatal("el grupo no se ha quitado")
	}
	if a := assetOf(t, s, d1); a.Price != nil || a.PurchaseOrder != "PO-2026-114" {
		t.Fatalf("precio borrado y pedido intacto: %+v", a)
	}
}
