package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func TestMergeAssets_ManualIntoAgent(t *testing.T) {
	s := storetest.New(t)
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw-m1", Hostname: "PC-AGENTE"})
	agent := assetOf(t, s, dev)
	_ = s.ApplyDirectorySync(ctx, []store.DirectoryUser{{EntraID: "e1", DisplayName: "Ana", Email: "ana@electrans.es", Department: "Finanzas", Enabled: true}}, time.Now())
	ana, _ := s.DirectoryUserByEntraID(ctx, "e1")
	price := 899.0
	manual, err := s.CreateAsset(ctx, store.AssetInput{CategoryID: category(t, s, "Portátil"), Name: "Portátil de Ana", Serial: "MAN-001",
		AssetTag: "EL-0042", Status: store.AssetInUse, OwnerID: &ana.ID, Location: "Sede Bilbao", Price: &price, Notes: "Comprado en 2025"})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := s.AddAssetDocument(ctx, store.AssetDocument{AssetID: manual.ID, SHA256: strings.Repeat("a", 64), Filename: "factura.pdf",
		ContentType: "application/pdf", Size: 10, UploadedAt: time.Now()})
	lic, err := s.CreateLicense(ctx, store.LicenseInput{Name: "Office", LicenseType: "suscripcion", Status: "activa", Seats: 5})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.AddLicenseAssignment(ctx, lic.ID, &manual.ID, nil, "")
	_, _ = s.AddLicenseAssignment(ctx, lic.ID, &agent.ID, nil, "") // la misma licencia en los dos: queda una

	// Se conserva la ficha del manual y el equipo del agente viene con ella.
	got, err := s.MergeAssets(ctx, store.AssetMerge{KeepID: manual.ID, OtherID: agent.ID,
		FromOther: map[string]bool{"name": true}, NotesBoth: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != manual.ID || got.DeviceID == nil || *got.DeviceID != dev || got.Name != agent.Name || got.Price == nil || *got.Price != 899 ||
		got.AssetTag != "EL-0042" || got.Location != "Sede Bilbao" || got.Department != "Finanzas" || got.Serial != "MAN-001" {
		t.Fatalf("activo fusionado: %+v", got)
	}
	if !strings.Contains(got.Notes, "Comprado en 2025") {
		t.Fatalf("notas: %q", got.Notes)
	}
	if a := assetOf(t, s, dev); a.ID != manual.ID {
		t.Fatal("el equipo apunta al activo conservado")
	}
	if dv, _ := s.DeviceByID(ctx, dev); dv.AssignedUser != "ana@electrans.es" {
		t.Fatalf("usuario del equipo: %q", dv.AssignedUser)
	}
	old, _ := s.AssetByID(ctx, agent.ID)
	if old.DeletedAt == nil || !strings.Contains(old.DeletedReason, "Fusionado") || old.DeviceID != nil {
		t.Fatalf("el otro va a la papelera: %+v", old)
	}
	if docs, _ := s.ListAssetDocuments(ctx, manual.ID); len(docs) != 1 || docs[0].ID != doc.ID {
		t.Fatalf("documentos: %+v", docs)
	}
	if as, _ := s.LicenseAssignments(ctx, lic.ID); len(as) != 1 {
		t.Fatalf("asignaciones de licencia sin duplicar: %+v", as)
	}
	// Elegir el número de serie del otro no choca con la unicidad.
	m2, _ := s.CreateAsset(ctx, store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "Monitor", Serial: "MON-9", Status: store.AssetInStock})
	m3, _ := s.CreateAsset(ctx, store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "Monitor bis", Status: store.AssetInStock})
	got, err = s.MergeAssets(ctx, store.AssetMerge{KeepID: m3.ID, OtherID: m2.ID, FromOther: map[string]bool{"serial": true}})
	if err != nil || got.Serial != "MON-9" {
		t.Fatalf("serie del otro: %+v %v", got, err)
	}
}

func TestMergeAssets_Refuses(t *testing.T) {
	s := storetest.New(t)
	a := assetOf(t, s, enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw-r1", Hostname: "A"}))
	b := assetOf(t, s, enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw-r2", Hostname: "B"}))
	if _, err := s.MergeAssets(ctx, store.AssetMerge{KeepID: a.ID, OtherID: b.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dos equipos con agente: %v", err)
	}
	if _, err := s.MergeAssets(ctx, store.AssetMerge{KeepID: a.ID, OtherID: a.ID}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("el mismo: %v", err)
	}
	m, _ := s.CreateAsset(ctx, store.AssetInput{CategoryID: category(t, s, "Monitor"), Name: "M", Status: store.AssetInStock})
	_ = s.TrashAsset(ctx, m.ID, nil, "x")
	if _, err := s.MergeAssets(ctx, store.AssetMerge{KeepID: a.ID, OtherID: m.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("en la papelera: %v", err)
	}
	if _, err := s.MergeAssets(ctx, store.AssetMerge{KeepID: a.ID, OtherID: m.ID, FromOther: map[string]bool{"inventado": true}}); err == nil {
		t.Fatal("campo desconocido")
	}
}
