package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

func seed(t *testing.T) (*store.Store, uuid.UUID, uuid.UUID) {
	s := storetest.New(t)
	a := storetest.SeedDevice(t, s, "PC-1", "windows")
	b := storetest.SeedDevice(t, s, "PC-2", "windows")
	_, _ = s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, a, time.Now())
	_, _ = s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, b, time.Now().Add(-45*24*time.Hour))
	_ = s.SaveInventory(ctx, a, protocol.Inventory{Hash: "a",
		Hardware: protocol.Hardware{Manufacturer: "Dell", Model: "Latitude 5440", Serial: "S1", CPUModel: "i5-1345U", MemoryBytes: 16 << 30},
		Disks:    []protocol.Disk{{Mount: "C:", SizeBytes: 512 << 30, FreeBytes: 100 << 30}},
		Software: []protocol.Software{{Name: "7-Zip", Version: "23.01"}, {Name: "=HYPERLINK(\"x\")", Version: "1"}}}, time.Now())
	_ = s.SaveInventory(ctx, b, protocol.Inventory{Hash: "b", Software: []protocol.Software{{Name: "7-Zip", Version: "23.01"}}}, time.Now())
	return s, a, b
}

func run(t *testing.T, s *store.Store, name string, f Filters) Result {
	t.Helper()
	r, err := Run(ctx, s, name, f, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReports_Content(t *testing.T) {
	s, _, _ := seed(t)
	hw := run(t, s, "hardware", Filters{})
	if hw.Total != 2 || hw.Columns[0] != "Equipo" {
		t.Fatalf("%+v", hw)
	}
	var pc1 []string
	for _, r := range hw.Rows {
		if r[0] == "PC-1" {
			pc1 = r
		}
	}
	if strings.Join(pc1, "|") != "PC-1|Dell|Latitude 5440|S1|i5-1345U|16|512||||" &&
		!strings.HasPrefix(strings.Join(pc1, "|"), "PC-1|Dell|Latitude 5440|S1|i5-1345U|16|512") {
		t.Fatalf("hardware: %v", pc1)
	}
	apps := run(t, s, "software_by_app", Filters{})
	if apps.Rows[0][0] != "7-Zip" || apps.Rows[0][2] != "2" {
		t.Fatalf("por programa (el más instalado primero): %v", apps.Rows)
	}
	byDev := run(t, s, "software_by_device", Filters{Q: "zip"})
	if byDev.Total != 2 {
		t.Fatalf("filtrado por nombre: %v", byDev.Rows)
	}
	inactive := run(t, s, "inactive", Filters{Days: 30})
	if inactive.Total != 1 || inactive.Rows[0][0] != "PC-2" {
		t.Fatalf("inactivos: %v", inactive.Rows)
	}
	page, _ := Run(ctx, s, "software_by_device", Filters{}, 1, 1)
	if len(page.Rows) != 1 || page.Total != 3 {
		t.Fatalf("paginación: %d de %d", len(page.Rows), page.Total)
	}
}

func TestReports_Definitions(t *testing.T) {
	want := []string{"hardware", "software_by_app", "software_by_device", "compliance", "patches_by_device", "patches_by_update", "deployments", "inactive", "android_patch_level"}
	for _, n := range want {
		if _, ok := Find(n); !ok {
			t.Errorf("falta el informe %s", n)
		}
	}
	if len(All()) != len(want) {
		t.Errorf("sobran informes")
	}
}

func TestCSV_BOMAndProtectedCells(t *testing.T) {
	var buf bytes.Buffer
	WriteCSV(&buf, []string{"Programa", "Equipos"}, [][]string{{"=SUMA(A1)", "2"}, {"7-Zip", "1"}})
	out := buf.String()
	if !strings.HasPrefix(out, "\xEF\xBB\xBF") || !strings.Contains(out, "'=SUMA(A1)") || !strings.Contains(out, "\r\n") {
		t.Fatalf("%q", out)
	}
}

func TestSchedule_Validate(t *testing.T) {
	if _, err := ParseFilters("software_by_device", json.RawMessage(`{"q":"zip","group_id":"no"}`)); err == nil {
		t.Fatal("un grupo inválido debe rechazarse")
	}
	if _, err := ParseFilters("inactive", json.RawMessage(`{"days":0}`)); err == nil {
		t.Fatal("días fuera de rango")
	}
	if f, err := ParseFilters("inactive", json.RawMessage(`{"days":30,"platform":"windows"}`)); err != nil || f.Days != 30 {
		t.Fatalf("%+v %v", f, err)
	}
}

// Todos los informes se ejecutan (SQL válido) con y sin filtros.
func TestReports_AllRun(t *testing.T) {
	s, _, _ := seed(t)
	g, _ := s.CreateGroup(ctx, "G", "")
	for _, d := range All() {
		for _, f := range []Filters{{}, {GroupID: &g.ID, Platform: "windows", Q: "x", Days: 5}} {
			r, err := Run(ctx, s, d.Name, f, 1, 10)
			if err != nil {
				t.Errorf("%s %+v: %v", d.Name, f, err)
				continue
			}
			for _, row := range r.Rows {
				if len(row) != len(d.Columns) {
					t.Errorf("%s: %d columnas, se esperaban %d", d.Name, len(row), len(d.Columns))
				}
			}
		}
	}
}

// La consola recorre los filtros: nunca deben ser null en el JSON.
func TestDefinitions_FiltersNeverNull(t *testing.T) {
	b, _ := json.Marshal(All())
	if strings.Contains(string(b), `"filters":null`) {
		t.Fatal("algún informe devuelve filters: null")
	}
}
