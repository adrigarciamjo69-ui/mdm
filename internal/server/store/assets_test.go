package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func category(t *testing.T, s *store.Store, name string) uuid.UUID {
	t.Helper()
	c, err := s.CategoryByName(ctx, name)
	if err != nil {
		t.Fatalf("categoría %s: %v", name, err)
	}
	return c.ID
}

func newAsset(t *testing.T, s *store.Store, in store.AssetInput) store.Asset {
	t.Helper()
	if in.Status == "" {
		in.Status = store.AssetInStock
	}
	a, err := s.CreateAsset(ctx, in)
	if err != nil {
		t.Fatalf("crear activo: %v", err)
	}
	return a
}

func TestCategories_SeededAndDeleteInUse(t *testing.T) {
	s := storetest.New(t)
	cats, err := s.ListCategories(ctx)
	if err != nil || len(cats) != 11 {
		t.Fatalf("categorías iniciales: %d %v", len(cats), err)
	}
	mon := category(t, s, "monitor") // sin distinguir mayúsculas
	newAsset(t, s, store.AssetInput{CategoryID: mon, Name: "Monitor 1"})
	if err := s.DeleteCategory(ctx, mon); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("borrar categoría en uso: %v", err)
	}
	c, err := s.CreateCategory(ctx, "Proyector")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, "proyector"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("categoría duplicada: %v", err)
	}
	if err := s.RenameCategory(ctx, c.ID, "Proyectores"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCategory(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAssets_CreateUpdateUnique(t *testing.T) {
	s := storetest.New(t)
	pc := category(t, s, "Portátil")
	price := 899.5
	a := newAsset(t, s, store.AssetInput{CategoryID: pc, Name: "Portátil Ana", Brand: "Lenovo", Model: "T14", Serial: " pf-3x9k2l ",
		AssetTag: "EL-0001", Price: &price})
	if a.Serial != "pf-3x9k2l" || a.CategoryName != "Portátil" || *a.Price != 899.5 || a.Status != store.AssetInStock {
		t.Fatalf("activo creado: %+v", a)
	}
	if _, err := s.CreateAsset(ctx, store.AssetInput{CategoryID: pc, Name: "Otro", Serial: "PF-3X9K2L", Status: store.AssetInUse}); !errors.Is(err, store.ErrSerialTaken) {
		t.Fatalf("serie repetido: %v", err)
	}
	if _, err := s.CreateAsset(ctx, store.AssetInput{CategoryID: pc, Name: "Otro", AssetTag: "el-0001", Status: store.AssetInUse}); !errors.Is(err, store.ErrTagTaken) {
		t.Fatalf("etiqueta repetida: %v", err)
	}
	// Los números de serie que no sirven no cuentan para la unicidad.
	newAsset(t, s, store.AssetInput{CategoryID: pc, Name: "A", Serial: "0"})
	newAsset(t, s, store.AssetInput{CategoryID: pc, Name: "B", Serial: "0"})

	if _, err := s.CreateAsset(ctx, store.AssetInput{CategoryID: pc, Name: "Mal", Status: "perdido"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("estado no válido: %v", err)
	}

	in := store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "Portátil Ana 2", Serial: "PF-3X9K2L", Status: store.AssetInRepair}
	u, err := s.UpdateAsset(ctx, a.ID, in)
	if err != nil || u.Name != "Portátil Ana 2" || u.CategoryName != "Monitor" || u.Status != store.AssetInRepair || u.Price != nil {
		t.Fatalf("actualizar: %+v %v", u, err)
	}
	if _, err := s.UpdateAsset(ctx, uuid.New(), in); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("actualizar inexistente: %v", err)
	}
}

func TestAssets_ListFilters(t *testing.T) {
	s := storetest.New(t)
	pc, mon := category(t, s, "Portátil"), category(t, s, "Monitor")
	newAsset(t, s, store.AssetInput{CategoryID: pc, Name: "Portátil Ana", Brand: "Lenovo", Serial: "SER-AAA1", Status: store.AssetInUse, Department: "TIC"})
	newAsset(t, s, store.AssetInput{CategoryID: mon, Name: "Monitor sala", Brand: "Dell", Model: "P2422H", AssetTag: "EL-9", Status: store.AssetInStock})

	list := func(f store.AssetFilter) []string {
		t.Helper()
		f.PerPage = 50
		rows, total, err := s.ListAssets(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if total != len(rows) {
			t.Fatalf("total %d, filas %d", total, len(rows))
		}
		var names []string
		for _, r := range rows {
			names = append(names, r.Name)
		}
		return names
	}
	if got := list(store.AssetFilter{Sort: "name"}); len(got) != 2 || got[0] != "Monitor sala" {
		t.Fatalf("todos: %v", got)
	}
	if got := list(store.AssetFilter{Origin: "manual"}); len(got) != 2 {
		t.Fatalf("manuales: %v", got)
	}
	if got := list(store.AssetFilter{CategoryID: &mon}); len(got) != 1 || got[0] != "Monitor sala" {
		t.Fatalf("categoría: %v", got)
	}
	if got := list(store.AssetFilter{Status: store.AssetInUse}); len(got) != 1 {
		t.Fatalf("estado en uso: %v", got)
	}
	for _, q := range []string{"aaa1", "dell", "p2422", "el-9", "sala"} {
		if got := list(store.AssetFilter{Q: q}); len(got) != 1 {
			t.Errorf("buscar %q: %v", q, got)
		}
	}
	if got := list(store.AssetFilter{Department: "TIC"}); len(got) != 1 {
		t.Fatalf("departamento: %v", got)
	}
}

func TestAssets_DepartmentsAndSort(t *testing.T) {
	s := storetest.New(t)
	cat := category(t, s, "Monitor")
	newAsset(t, s, store.AssetInput{CategoryID: cat, Name: "M1", Status: store.AssetInUse, Department: "Taller"})
	newAsset(t, s, store.AssetInput{CategoryID: cat, Name: "M2", Status: store.AssetInUse, Department: "Finanzas"})
	newAsset(t, s, store.AssetInput{CategoryID: cat, Name: "M3", Status: store.AssetInUse, Department: "Taller"})
	newAsset(t, s, store.AssetInput{CategoryID: cat, Name: "M4", Status: store.AssetInUse})
	deps, err := s.AssetDepartments(ctx)
	if err != nil || len(deps) != 2 || deps[0] != "Finanzas" || deps[1] != "Taller" {
		t.Fatalf("departamentos: %v %v", deps, err)
	}
	if !store.ValidAssetSort("department") {
		t.Fatal("ordenar por departamento")
	}
	rows, n, err := s.ListAssets(ctx, store.AssetFilter{Department: "Taller", Sort: "department", PerPage: 10})
	if err != nil || n != 2 || len(rows) != 2 {
		t.Fatalf("filtro: n=%d err=%v", n, err)
	}
}
