package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

func TestCreateAdmin_DuplicateEmailCaseInsensitive(t *testing.T) {
	s := storetest.New(t)
	if _, err := s.CreateAdmin(ctx, "A@x.es", "h", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdmin(ctx, "a@x.es", "h", "admin"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("se esperaba ErrConflict, got %v", err)
	}
	got, err := s.AdminByEmail(ctx, "a@X.ES")
	if err != nil || got.Email != "A@x.es" {
		t.Fatalf("búsqueda sin distinguir mayúsculas: %+v %v", got, err)
	}
}

func TestUpdateAdmin_CannotRemoveLastAdmin(t *testing.T) {
	s := storetest.New(t)
	a, _ := s.CreateAdmin(ctx, "a@x.es", "h", "admin")
	op, _ := s.CreateAdmin(ctx, "op@x.es", "h", "operator")
	yes := true
	if err := s.UpdateAdmin(ctx, a.ID, store.AdminPatch{Disabled: &yes}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("desactivar al único admin: %v", err)
	}
	role := "operator"
	if err := s.UpdateAdmin(ctx, a.ID, store.AdminPatch{Role: &role}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("degradar al único admin: %v", err)
	}
	still, _ := s.AdminByID(ctx, a.ID)
	if still.Disabled || still.Role != "admin" {
		t.Errorf("el cambio rechazado se aplicó: %+v", still)
	}
	// Con un segundo administrador sí se puede.
	admin := "admin"
	if err := s.UpdateAdmin(ctx, op.ID, store.AdminPatch{Role: &admin}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAdmin(ctx, a.ID, store.AdminPatch{Disabled: &yes}); err != nil {
		t.Errorf("con dos admins debería poder desactivarse uno: %v", err)
	}
}

func TestUpdateAdmin_NotFound(t *testing.T) {
	s := storetest.New(t)
	a, _ := s.CreateAdmin(ctx, "a@x.es", "h", "admin")
	_ = s.DeleteAdminForTest(ctx, a.ID)
	yes := true
	if err := s.UpdateAdmin(ctx, a.ID, store.AdminPatch{Disabled: &yes}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func newSession(adminID [16]byte, hash string, now time.Time) store.Session {
	return store.Session{TokenHash: hash, AdminID: adminID, CSRFToken: "c", CreatedAt: now, ExpiresAt: now.Add(12 * time.Hour), IP: "1.2.3.4"}
}

func TestSession_ExpiredNotFound(t *testing.T) {
	s := storetest.New(t)
	a, _ := s.CreateAdmin(ctx, "a@x.es", "h", "admin")
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if err := s.CreateSession(ctx, newSession(a.ID, "h1", now)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionByTokenHash(ctx, "h1", now.Add(12*time.Hour-time.Second)); err != nil {
		t.Fatalf("antes de caducar: %v", err)
	}
	if _, _, err := s.SessionByTokenHash(ctx, "h1", now.Add(12*time.Hour+time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tras caducar: %v", err)
	}
}

func TestSession_DisabledAdminNotFound(t *testing.T) {
	s := storetest.New(t)
	a, _ := s.CreateAdmin(ctx, "a@x.es", "h", "admin")
	b, _ := s.CreateAdmin(ctx, "b@x.es", "h", "admin")
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, newSession(b.ID, "h1", now))
	yes := true
	if err := s.UpdateAdmin(ctx, b.ID, store.AdminPatch{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionByTokenHash(ctx, "h1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	_ = a
}

func TestSession_VerifyAndDelete(t *testing.T) {
	s := storetest.New(t)
	a, _ := s.CreateAdmin(ctx, "a@x.es", "h", "admin")
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, newSession(a.ID, "h1", now))
	sess, adm, _ := s.SessionByTokenHash(ctx, "h1", now)
	if sess.Verified || adm.ID != a.ID {
		t.Fatalf("sesión nueva: %+v", sess)
	}
	_ = s.VerifySession(ctx, "h1")
	sess, _, _ = s.SessionByTokenHash(ctx, "h1", now)
	if !sess.Verified {
		t.Fatal("no quedó verificada")
	}
	_ = s.DeleteSession(ctx, "h1")
	if _, _, err := s.SessionByTokenHash(ctx, "h1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tras borrar: %v", err)
	}
}

func TestAudit_InsertAndList(t *testing.T) {
	s := storetest.New(t)
	base := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	for i, a := range []string{"login.ok", "device.revoked", "login.ok"} {
		if err := s.InsertAudit(ctx, store.AuditEntry{At: base.Add(time.Duration(i) * time.Minute), ActorEmail: "a@x.es", Action: a, Details: []byte(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	all, total, err := s.ListAudit(ctx, store.AuditFilter{Page: 1, PerPage: 2})
	if err != nil || total != 3 || len(all) != 2 || !all[0].At.After(all[1].At) {
		t.Fatalf("total=%d len=%d err=%v", total, len(all), err)
	}
	only, total, _ := s.ListAudit(ctx, store.AuditFilter{Action: "device.revoked", Page: 1, PerPage: 10})
	if total != 1 || only[0].Action != "device.revoked" {
		t.Fatalf("filtro por acción: %d", total)
	}
	from := base.Add(90 * time.Second)
	_, total, _ = s.ListAudit(ctx, store.AuditFilter{From: &from, Page: 1, PerPage: 10})
	if total != 1 {
		t.Fatalf("filtro por fecha: %d", total)
	}
}
