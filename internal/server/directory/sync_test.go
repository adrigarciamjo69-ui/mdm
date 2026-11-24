package directory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"mdm/internal/server/directory"
	"mdm/internal/server/directory/fake"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

func setup(t *testing.T) (*store.Store, *fake.Server, *directory.Syncer) {
	t.Helper()
	s := storetest.New(t)
	f := fake.New()
	t.Cleanup(f.Close)
	box, err := secrets.New("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := box.Seal([]byte("secreto"))
	if err := s.SaveEntraConfig(ctx, store.EntraConfig{TenantID: "electrans", ClientID: "app", SealedSecret: sealed}); err != nil {
		t.Fatal(err)
	}
	f.Users = []fake.User{
		{ID: "u1", DisplayName: "Ana Pérez", Mail: "ana@electrans.es", UserPrincipalName: "ana@electrans.es", Department: "TIC", AccountEnabled: true},
		{ID: "u2", DisplayName: "Luis Gil", UserPrincipalName: "luis@electrans.es", JobTitle: "Técnico", AccountEnabled: true},
		{ID: "u3", DisplayName: "Marta Ruiz", Mail: "marta@electrans.es", AccountEnabled: false},
	}
	sy := directory.NewSyncer(directory.Deps{Store: s, Secrets: box, AuthBase: f.AuthBase(), GraphBase: f.GraphBase(), Now: time.Now})
	return s, f, sy
}

func names(t *testing.T, s *store.Store, status string) []string {
	t.Helper()
	rows, _, err := s.ListDirectoryUsers(ctx, store.DirectoryFilter{Status: status, PerPage: 100})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.DisplayName)
	}
	return out
}

func TestSync_PaginatesAndMaps(t *testing.T) {
	s, f, sy := setup(t)
	if err := sy.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := names(t, s, ""); len(got) != 3 {
		t.Fatalf("usuarios: %v", got)
	}
	luis, err := s.DirectoryUserByEntraID(ctx, "u2")
	if err != nil || luis.Contact() != "luis@electrans.es" || luis.JobTitle != "Técnico" || !luis.Enabled {
		t.Fatalf("Luis: %+v %v", luis, err)
	}
	if got := names(t, s, "disabled"); len(got) != 1 || got[0] != "Marta Ruiz" {
		t.Fatalf("deshabilitados: %v", got)
	}
	q := f.Queries[0]
	if !strings.Contains(q, "userType+eq+%27Member%27") && !strings.Contains(q, "userType%20eq%20%27Member%27") {
		t.Fatalf("falta el filtro de miembros: %s", q)
	}
	cfg, _ := s.EntraConfig(ctx)
	if cfg.LastError != "" || cfg.LastCount != 3 || cfg.LastSuccessAt == nil {
		t.Fatalf("estado: %+v", cfg)
	}
}

func TestSync_MarksDeletedAndRestores(t *testing.T) {
	s, f, sy := setup(t)
	_ = sy.Run(ctx)
	f.Users = f.Users[:2]
	_ = sy.Run(ctx)
	if got := names(t, s, "deleted"); len(got) != 1 || got[0] != "Marta Ruiz" {
		t.Fatalf("eliminados: %v", got)
	}
	f.Users = append(f.Users, fake.User{ID: "u3", DisplayName: "Marta Ruiz", AccountEnabled: true})
	_ = sy.Run(ctx)
	if got := names(t, s, "deleted"); len(got) != 0 {
		t.Fatalf("sigue eliminada: %v", got)
	}
}

func TestSync_ErrorMidwayChangesNothing(t *testing.T) {
	s, f, sy := setup(t)
	_ = sy.Run(ctx)
	f.Users = f.Users[:1]
	f.Users = append(f.Users, fake.User{ID: "u9", DisplayName: "Nuevo", AccountEnabled: true}, fake.User{ID: "u10", DisplayName: "Otro", AccountEnabled: true})
	f.FailPage = 2
	if err := sy.Run(ctx); err != nil {
		t.Fatalf("un fallo de Graph no es un error de la tarea: %v", err)
	}
	if got := names(t, s, "deleted"); len(got) != 0 {
		t.Fatalf("se ha marcado a alguien como eliminado: %v", got)
	}
	if got := names(t, s, ""); len(got) != 3 {
		t.Fatalf("usuarios: %v", got)
	}
	cfg, _ := s.EntraConfig(ctx)
	if !strings.Contains(cfg.LastError, "Fallo simulado") || cfg.LastSyncAt == nil {
		t.Fatalf("error guardado: %+v", cfg)
	}
}

func TestSync_BadSecretRecordsError(t *testing.T) {
	s, f, sy := setup(t)
	f.Secret = "otro"
	_ = sy.Run(ctx)
	cfg, _ := s.EntraConfig(ctx)
	if !strings.Contains(cfg.LastError, "AADSTS7000215") {
		t.Fatalf("error: %q", cfg.LastError)
	}
}

func TestSync_NotConfiguredDoesNothing(t *testing.T) {
	s := storetest.New(t)
	sy := directory.NewSyncer(directory.Deps{Store: s, Now: time.Now})
	if err := sy.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
