package store_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func sessionInventory(t *testing.T, s *store.Store, dev uuid.UUID, login string) {
	t.Helper()
	inv := protocol.Inventory{CollectedAt: time.Now(), System: protocol.System{Hostname: "2138P", LoggedInUser: login}}
	if err := s.SaveInventory(ctx, dev, inv, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func ownerOf(a store.Asset) string {
	if a.OwnerID == nil {
		return ""
	}
	return a.OwnerID.String()
}

func TestSessionUser_SetsOwnerAndDepartment(t *testing.T) {
	s := storetest.New(t)
	if err := s.ApplyDirectorySync(ctx, []store.DirectoryUser{
		{EntraID: "e1", DisplayName: "Ferran Sarerols", Email: "fsarerols@electrans.es", UPN: "fsarerols@electrans.es", Department: "Taller", Enabled: true},
		{EntraID: "e2", DisplayName: "Ana Pérez", Email: "ana.perez@electrans.es", UPN: "ana.perez@electrans.es", SamAccount: "aperez", Department: "Finanzas", Enabled: true},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ferran, _ := s.DirectoryUserByEntraID(ctx, "e1")
	ana, _ := s.DirectoryUserByEntraID(ctx, "e2")
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "2138P"})

	// Por la parte del UPN antes de la arroba.
	sessionInventory(t, s, dev, `ELECTRANS\fsarerols`)
	a := assetOf(t, s, dev)
	if ownerOf(a) != ferran.ID.String() || a.Department != "Taller" {
		t.Fatalf("persona por sesión: %+v", a)
	}
	if d, _ := s.DeviceByID(ctx, dev); d.AssignedUser != "fsarerols@electrans.es" {
		t.Fatalf("usuario asignado del equipo: %q", d.AssignedUser)
	}
	// Por el usuario de Windows de AD (onPremisesSamAccountName).
	sessionInventory(t, s, dev, `ELECTRANS\APEREZ`)
	if a := assetOf(t, s, dev); ownerOf(a) != ana.ID.String() || a.Department != "Finanzas" {
		t.Fatalf("persona por sAMAccountName: %+v", a)
	}
	// Una cuenta local o sin sesión no toca nada.
	sessionInventory(t, s, dev, `2138P\Administrador`)
	sessionInventory(t, s, dev, "")
	if a := assetOf(t, s, dev); ownerOf(a) != ana.ID.String() {
		t.Fatalf("cuenta desconocida cambió la persona: %+v", a)
	}
}

func TestSessionUser_RespectsLocks(t *testing.T) {
	s := storetest.New(t)
	if err := s.ApplyDirectorySync(ctx, []store.DirectoryUser{
		{EntraID: "e1", DisplayName: "Ferran", Email: "fsarerols@electrans.es", UPN: "fsarerols@electrans.es", Department: "Taller", Enabled: true},
		{EntraID: "e2", DisplayName: "Ana", Email: "aperez@electrans.es", UPN: "aperez@electrans.es", Department: "Finanzas", Enabled: true},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ana, _ := s.DirectoryUserByEntraID(ctx, "e2")
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "2138P"})
	a := assetOf(t, s, dev)

	// Persona bloqueada en Ana y departamento bloqueado a mano.
	in := store.AssetInput{CategoryID: a.CategoryID, Name: a.Name, Status: a.Status, OwnerID: &ana.ID, Department: "Dirección",
		OwnerLocked: true, DepartmentLocked: true}
	if _, err := s.UpdateAsset(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	if a := assetOf(t, s, dev); a.Department != "Dirección" || !a.OwnerLocked || !a.DepartmentLocked {
		t.Fatalf("bloqueos guardados: %+v", a)
	}
	sessionInventory(t, s, dev, `ELECTRANS\fsarerols`)
	if a := assetOf(t, s, dev); ownerOf(a) != ana.ID.String() || a.Department != "Dirección" {
		t.Fatalf("la sesión no debe cambiar lo bloqueado: %+v", a)
	}
	// La sincronización con Entra tampoco pisa el departamento bloqueado.
	if err := s.ApplyDirectorySync(ctx, []store.DirectoryUser{
		{EntraID: "e1", DisplayName: "Ferran", Email: "fsarerols@electrans.es", UPN: "fsarerols@electrans.es", Department: "Taller", Enabled: true},
		{EntraID: "e2", DisplayName: "Ana", Email: "aperez@electrans.es", UPN: "aperez@electrans.es", Department: "Compras", Enabled: true},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a := assetOf(t, s, dev); a.Department != "Dirección" {
		t.Fatalf("sincronización pisó el departamento bloqueado: %q", a.Department)
	}

	// Solo el departamento bloqueado: la persona sigue la sesión.
	in.OwnerLocked = false
	if _, err := s.UpdateAsset(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	sessionInventory(t, s, dev, `ELECTRANS\fsarerols`)
	if a := assetOf(t, s, dev); a.OwnerID == nil || *a.OwnerID == ana.ID || a.Department != "Dirección" {
		t.Fatalf("persona libre y departamento bloqueado: %+v", a)
	}
}

func TestSessionUser_AmbiguousDoesNothing(t *testing.T) {
	s := storetest.New(t)
	if err := s.ApplyDirectorySync(ctx, []store.DirectoryUser{
		{EntraID: "e1", DisplayName: "Juan A", Email: "jgarcia@electrans.es", UPN: "jgarcia@electrans.es", Enabled: true},
		{EntraID: "e2", DisplayName: "Juan B", Email: "jgarcia@filial.es", UPN: "jgarcia@filial.es", Enabled: true},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	dev := enrollWith(t, s, protocol.EnrollRequest{HardwareID: "hw1", Hostname: "PC"})
	sessionInventory(t, s, dev, `ELECTRANS\jgarcia`)
	if a := assetOf(t, s, dev); a.OwnerID != nil {
		t.Fatalf("ambiguo: no debe asignar a nadie: %+v", a)
	}
	// Con el UPN completo sí hay una exacta.
	sessionInventory(t, s, dev, "jgarcia@filial.es")
	b, _ := s.DirectoryUserByEntraID(ctx, "e2")
	if a := assetOf(t, s, dev); ownerOf(a) != b.ID.String() {
		t.Fatalf("UPN exacto: %+v", a)
	}
}
