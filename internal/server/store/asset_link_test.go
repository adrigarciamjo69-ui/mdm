package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

// enrollWith inscribe un equipo con los datos indicados.
func enrollWith(t *testing.T, s *store.Store, req protocol.EnrollRequest) uuid.UUID {
	t.Helper()
	adm, err := s.AdminByEmail(ctx, "a@ejemplo.es")
	if err != nil {
		adm, _ = s.CreateAdmin(ctx, "a@ejemplo.es", "x", "admin")
	}
	hash := "tok-" + uuid.NewString()
	if _, err := s.CreateToken(ctx, store.TokenParams{Name: "t", TokenHash: hash, CreatedBy: adm.ID}); err != nil {
		t.Fatal(err)
	}
	if req.Platform == "" {
		req.Platform = "windows"
	}
	res, err := s.EnrollDevice(ctx, store.EnrollParams{TokenHash: hash, CredentialHash: "c", At: time.Now(), Req: req})
	if err != nil {
		t.Fatal(err)
	}
	return res.DeviceID
}

func assetOf(t *testing.T, s *store.Store, dev uuid.UUID) store.Asset {
	t.Helper()
	a, err := s.AssetByDevice(ctx, dev)
	if err != nil {
		t.Fatalf("activo del equipo: %v", err)
	}
	return a
}

func countAssets(t *testing.T, s *store.Store) int {
	t.Helper()
	_, n, err := s.ListAssets(ctx, store.AssetFilter{PerPage: 1})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnroll_CreatesAsset(t *testing.T) {
	s := storetest.New(t)
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC-ANA", OSName: "Windows 11 Pro",
		Serial: "PF1ABCD", Manufacturer: "LENOVO", Model: "T14"})
	a := assetOf(t, s, dev)
	if a.Name != "PC-ANA" || a.CategoryName != "Ordenador" || !a.CategoryAuto || a.Status != store.AssetInUse ||
		a.Serial != "PF1ABCD" || a.Brand != "LENOVO" || a.Model != "T14" {
		t.Fatalf("activo: %+v", a)
	}
	srv := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw2", Hostname: "SRV1", OSName: "Windows Server 2022 Standard"})
	if c := assetOf(t, s, srv).CategoryName; c != "Servidor" {
		t.Fatalf("servidor: %s", c)
	}
	rows, _, _ := s.ListAssets(ctx, store.AssetFilter{Origin: "agent", Sort: "name", PerPage: 10})
	if len(rows) != 2 || rows[0].Device == nil || rows[0].Device.ID != dev || rows[0].Device.Hostname != "PC-ANA" {
		t.Fatalf("listado con agente: %+v", rows)
	}
	if got, _, _ := s.ListAssets(ctx, store.AssetFilter{Device: store.DeviceFilter{Platform: "linux"}, PerPage: 10}); len(got) != 0 {
		t.Fatalf("filtro de plataforma: %d", len(got))
	}
}

func TestEnroll_LinksManualAssetBySerial(t *testing.T) {
	s := storetest.New(t)
	price := 1200.0
	manual := newAsset(t, s, store.AssetInput{CategoryID: category(t, s, "Portátil"), Name: "Portátil de Ana", Serial: "pf1abcd ",
		Price: &price, Status: store.AssetInStock, Location: "Almacén"})
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC-ANA", Serial: "PF1ABCD", Manufacturer: "LENOVO", Model: "T14"})
	a := assetOf(t, s, dev)
	if a.ID != manual.ID || a.Name != "Portátil de Ana" || *a.Price != 1200 || a.Status != store.AssetInStock ||
		a.CategoryName != "Portátil" || a.Brand != "LENOVO" || a.Model != "T14" || a.Location != "Almacén" {
		t.Fatalf("unión: %+v", a)
	}
	if n := countAssets(t, s); n != 1 {
		t.Fatalf("hay %d activos, se esperaba 1", n)
	}
}

func TestEnroll_JunkSerialDoesNotLink(t *testing.T) {
	s := storetest.New(t)
	manual := newAsset(t, s, store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "Monitor", Serial: "0"})
	d1 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "VM1", Serial: "0"})
	d2 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw2", Hostname: "VM2", Serial: "To be filled by O.E.M."})
	d3 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw3", Hostname: "VM3", Serial: "To be filled by O.E.M."})
	if assetOf(t, s, d1).ID == manual.ID || assetOf(t, s, d2).ID == assetOf(t, s, d3).ID {
		t.Fatal("se han unido activos por un número de serie basura")
	}
	if n := countAssets(t, s); n != 4 {
		t.Fatalf("hay %d activos, se esperaban 4", n)
	}
}

func TestEnroll_ReenrollNoDuplicate(t *testing.T) {
	s := storetest.New(t)
	req := protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1", Serial: "SER12345"}
	d := enrollWith(t, s, req)
	first := assetOf(t, s, d)
	req.Hostname = "PC1-NUEVO"
	if enrollWith(t, s, req) != d || assetOf(t, s, d).ID != first.ID || countAssets(t, s) != 1 {
		t.Fatal("la reinscripción ha duplicado el activo")
	}
}

func TestAndroidUpsert_CreatesAsset(t *testing.T) {
	s := storetest.New(t)
	id, _, err := s.UpsertAndroidDevice(ctx, store.AndroidDevice{AmapiName: "enterprises/e/devices/1", Hostname: "Galaxy A55",
		Serial: "R58M123456", Manufacturer: "samsung", Model: "SM-A556B", EnrolledAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if a := assetOf(t, s, id); a.CategoryName != "Móvil" || a.Serial != "R58M123456" {
		t.Fatalf("móvil: %+v", a)
	}
	if _, _, err := s.UpsertAndroidDevice(ctx, store.AndroidDevice{AmapiName: "enterprises/e/devices/1", Hostname: "Galaxy A55",
		Serial: "R58M123456", EnrolledAt: time.Now()}); err != nil || countAssets(t, s) != 1 {
		t.Fatalf("segunda sincronización: %v", err)
	}
}

func TestInventory_ChassisSetsCategoryOnlyWhenAuto(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1"})
	save := func(chassis string) {
		t.Helper()
		if err := s.SaveInventory(ctx, d, protocol.Inventory{CollectedAt: time.Now(), Hardware: protocol.Hardware{Chassis: chassis}}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	save("laptop")
	a := assetOf(t, s, d)
	if a.CategoryName != "Portátil" || !a.CategoryAuto {
		t.Fatalf("tras inventario: %+v", a)
	}
	in := store.AssetInput{CategoryID: category(t, s, "Tableta"), Name: a.Name, Status: a.Status, Serial: a.Serial}
	if _, err := s.UpdateAsset(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	save("desktop")
	if a := assetOf(t, s, d); a.CategoryName != "Tableta" || a.CategoryAuto {
		t.Fatalf("la categoría elegida a mano ha cambiado: %+v", a)
	}
}

func TestAsset_OwnerMirrorsDeviceUser(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1"})
	if err := s.ApplyDirectorySync(ctx, []store.DirectoryUser{{EntraID: "e1", DisplayName: "Ana Pérez", Email: "ana@electrans.es", Enabled: true},
		{EntraID: "e2", DisplayName: "Sin correo", UPN: "sc@electrans.onmicrosoft.com", Enabled: true}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ana, _ := s.DirectoryUserByEntraID(ctx, "e1")
	sc, _ := s.DirectoryUserByEntraID(ctx, "e2")
	a := assetOf(t, s, d)
	in := store.AssetInput{CategoryID: a.CategoryID, Name: a.Name, Status: a.Status, OwnerID: &ana.ID}
	if _, err := s.UpdateAsset(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	if dv, _ := s.DeviceByID(ctx, d); dv.AssignedUser != "ana@electrans.es" {
		t.Fatalf("usuario del equipo: %q", dv.AssignedUser)
	}
	in.OwnerID = &sc.ID
	_, _ = s.UpdateAsset(ctx, a.ID, in)
	if dv, _ := s.DeviceByID(ctx, d); dv.AssignedUser != "sc@electrans.onmicrosoft.com" {
		t.Fatalf("usuario sin correo: %q", dv.AssignedUser)
	}
	in.OwnerID = nil
	_, _ = s.UpdateAsset(ctx, a.ID, in)
	if dv, _ := s.DeviceByID(ctx, d); dv.AssignedUser != "" {
		t.Fatalf("sin dueño: %q", dv.AssignedUser)
	}
}

func TestManualSerialLinksAgentDeviceWithoutAsset(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1", Serial: "SER12345"})
	// Otro activo con el serie del equipo choca con el del equipo.
	if _, err := s.CreateAsset(ctx, store.AssetInput{CategoryID: category(t, s, "Portátil"), Name: "Duplicado", Serial: "ser12345",
		Status: store.AssetInUse}); !errors.Is(err, store.ErrSerialTaken) {
		t.Fatalf("serie del equipo: %v", err)
	}
	_ = d
}

func TestDeleteDevice_KeepsAsset(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1", Serial: "SER12345"})
	a := assetOf(t, s, d)
	if err := s.DeleteDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	b, err := s.AssetByID(ctx, a.ID)
	if err != nil || b.DeviceID != nil || b.Serial != "SER12345" {
		t.Fatalf("activo tras borrar el equipo: %+v %v", b, err)
	}
	// Si el equipo vuelve, se une de nuevo por número de serie.
	d2 := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1", Serial: "SER12345"})
	if assetOf(t, s, d2).ID != a.ID {
		t.Fatal("no se ha vuelto a unir por número de serie")
	}
}

func TestLinkUnlink(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1", Serial: "SER12345"})
	auto := assetOf(t, s, d)
	manual := newAsset(t, s, store.AssetInput{CategoryID: category(t, s, "Portátil"), Name: "Portátil comprado", Serial: "OTRO-1234"})

	// Enlazar a mano sustituye el activo automático del equipo, que va a la papelera.
	if err := s.LinkAsset(ctx, manual.ID, d, nil); err != nil {
		t.Fatal(err)
	}
	if assetOf(t, s, d).ID != manual.ID {
		t.Fatal("no se ha enlazado")
	}
	if old, _ := s.AssetByID(ctx, auto.ID); old.DeletedAt == nil || old.DeviceID != nil {
		t.Fatalf("el activo sustituido no ha ido a la papelera: %+v", old)
	}
	if err := s.LinkAsset(ctx, manual.ID, d, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("enlazar dos veces: %v", err)
	}

	// Desenlazar deja el activo como manual y el equipo recibe uno nuevo.
	if err := s.UnlinkAsset(ctx, manual.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := s.AssetByID(ctx, manual.ID)
	if m.DeviceID != nil {
		t.Fatal("sigue enlazado")
	}
	if fresh := assetOf(t, s, d); fresh.ID == manual.ID || fresh.ID == auto.ID {
		t.Fatalf("el equipo no tiene un activo nuevo: %+v", fresh)
	}
	if err := s.UnlinkAsset(ctx, manual.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("desenlazar un activo manual: %v", err)
	}
}

func TestEnsureAllDeviceAssets_Backfill(t *testing.T) {
	s := storetest.New(t)
	d := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC1"})
	a := assetOf(t, s, d)
	if err := s.PurgeAssetForTest(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	n, err := s.EnsureAllDeviceAssets(ctx)
	if err != nil || n != 1 {
		t.Fatalf("creados %d: %v", n, err)
	}
	assetOf(t, s, d)
	if n, _ := s.EnsureAllDeviceAssets(ctx); n != 0 {
		t.Fatalf("segunda pasada: %d", n)
	}
}
