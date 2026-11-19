package store_test

import (
	"context"
	"testing"

	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

func TestMigrate_CreatesAllTables(t *testing.T) {
	s := storetest.New(t)
	for _, tbl := range []string{"admins", "sessions", "groups", "enrollment_tokens", "devices",
		"device_inventory", "device_software", "device_activity", "commands", "audit_log"} {
		if !s.TableExists(context.Background(), tbl) {
			t.Errorf("falta la tabla %s", tbl)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	_, url := storetest.NewWithURL(t)
	if err := store.Migrate(context.Background(), url); err != nil {
		t.Fatalf("segunda migración: %v", err)
	}
}

func TestPing(t *testing.T) {
	s := storetest.New(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
