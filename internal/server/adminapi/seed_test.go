package adminapi

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type seed struct {
	Hostname, Platform, OSName, OSVersion, Serial, User string
	Tags                                                []string
	Group                                               *uuid.UUID
	SeenAgo                                             time.Duration // 0 = nunca ha conectado
	Revoked                                             bool
	Inventory                                           *protocol.Inventory
}

// seedDevice crea un dispositivo directamente en el almacén.
func (a *testAPI) seedDevice(s seed) uuid.UUID {
	a.t.Helper()
	a.n++
	_, hash, _ := auth.NewSecret()
	tok, err := a.store.CreateToken(ctx, store.TokenParams{Name: "semilla", TokenHash: hash, CreatedBy: a.seedAdmin()})
	if err != nil {
		a.t.Fatal(err)
	}
	_ = tok
	if s.Platform == "" {
		s.Platform = "windows"
	}
	if s.Hostname == "" {
		s.Hostname = fmt.Sprintf("PC-%03d", a.n)
	}
	res, err := a.store.EnrollDevice(ctx, store.EnrollParams{
		TokenHash: hash, CredentialHash: "h", IP: "10.0.0.1", At: a.clock.Now().Add(-time.Duration(1000-a.n) * time.Minute),
		Req: protocol.EnrollRequest{HardwareID: fmt.Sprintf("hw-%d", a.n), Hostname: s.Hostname, Platform: s.Platform,
			OSName: s.OSName, OSVersion: s.OSVersion, Serial: s.Serial, Arch: "amd64", AgentVersion: "1.0.0"},
	})
	if err != nil {
		a.t.Fatal(err)
	}
	id := res.DeviceID
	patch := store.DevicePatch{}
	if s.Group != nil {
		g := s.Group
		patch.GroupID = &g
	}
	if s.User != "" {
		patch.AssignedUser = &s.User
	}
	if s.Tags != nil {
		patch.Tags = &s.Tags
	}
	if err := a.store.UpdateDevice(ctx, id, patch); err != nil {
		a.t.Fatal(err)
	}
	if s.SeenAgo > 0 {
		if err := a.store.RecordCheckin(ctx, id, store.CheckinParams{AgentVersion: "1.0.0", IP: "10.0.0.1", At: a.clock.Now().Add(-s.SeenAgo)}); err != nil {
			a.t.Fatal(err)
		}
	}
	if s.Inventory != nil {
		inv := *s.Inventory
		store.SanitizeInventory(&inv)
		if err := a.store.SaveInventory(ctx, id, inv, a.clock.Now()); err != nil {
			a.t.Fatal(err)
		}
	}
	if s.Revoked {
		if err := a.store.RevokeDevice(ctx, id, a.clock.Now()); err != nil {
			a.t.Fatal(err)
		}
	}
	return id
}

func (a *testAPI) seedAdmin() uuid.UUID {
	adm, err := a.store.AdminByEmail(ctx, "semilla@ejemplo.es")
	if err == nil {
		return adm.ID
	}
	adm, err = a.store.CreateAdmin(ctx, "semilla@ejemplo.es", "x", "admin")
	if err != nil {
		a.t.Fatal(err)
	}
	return adm.ID
}

func invWith(freePct int, encryption string) *protocol.Inventory {
	size := uint64(100 << 30)
	return &protocol.Inventory{
		CollectedAt: time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC),
		Hash:        "sha256:x",
		System:      protocol.System{Kernel: "6.8"},
		Disks:       []protocol.Disk{{Device: "C:", SizeBytes: size, FreeBytes: size * uint64(freePct) / 100}},
		Security:    protocol.Security{DiskEncryption: encryption, Firewall: "on"},
		Software:    []protocol.Software{{Name: "Firefox", Version: "132"}, {Name: "Git", Version: "2.47"}, {Name: "7-Zip", Version: "24"}},
	}
}

func storeTags(t *[]string) store.DevicePatch { return store.DevicePatch{Tags: t} }

func mustUUID(s string) uuid.UUID { return uuid.MustParse(s) }
