package agentapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

type agentClient struct {
	a     *agentTest
	id    uuid.UUID
	authz string
}

// enrolled inscribe un equipo nuevo y devuelve un cliente con su credencial.
func (a *agentTest) enrolled() *agentClient {
	a.t.Helper()
	a.n++
	resp, out := a.enrollFrom(fmt.Sprintf("203.0.113.%d", 100+a.n%100), sampleEnroll(a.token(nil, nil), fmt.Sprintf("hw-auto-%d", a.n)))
	if resp.StatusCode != http.StatusCreated {
		a.t.Fatalf("inscripción de prueba: %d %s", resp.StatusCode, bodyString(resp))
	}
	return &agentClient{a: a, id: uuid.MustParse(out.DeviceID), authz: "Bearer " + auth.FormatDeviceCredential(out.DeviceID, out.Credential)}
}

func (c *agentClient) checkin(req protocol.CheckinRequest) (*http.Response, protocol.CheckinResponse) {
	c.a.t.Helper()
	resp := c.a.post("/checkin", "203.0.113.50", c.authz, req, false)
	var out protocol.CheckinResponse
	if resp.StatusCode == http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &out); err != nil {
			c.a.t.Fatalf("respuesta de check-in: %s", b)
		}
	}
	return resp, out
}

func (c *agentClient) inventory(inv protocol.Inventory) *http.Response {
	return c.a.post("/inventory", "203.0.113.50", c.authz, inv, true)
}

func sampleInventory() protocol.Inventory {
	inst := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	inv := protocol.Inventory{
		CollectedAt: time.Date(2026, 11, 13, 11, 59, 0, 0, time.UTC),
		Unavailable: []string{},
		System:      protocol.System{Hostname: "PC-NUEVO", OSName: "Windows 11 Pro", OSVersion: "10.0.26200", Arch: "amd64", LoggedInUser: "ana"},
		Hardware:    protocol.Hardware{Manufacturer: "Lenovo", Model: "ThinkPad T14", Serial: "PF123", CPUModel: "Ryzen 7", CPUCores: 8, CPUThreads: 16, MemoryBytes: 16 << 30},
		Disks:       []protocol.Disk{{Device: "C:", Mount: "C:", Filesystem: "NTFS", SizeBytes: 500 << 30, FreeBytes: 200 << 30, Encrypted: true}},
		Network:     []protocol.NetIf{{Name: "Ethernet", MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"10.0.0.5"}}},
		Security:    protocol.Security{DiskEncryption: "on", Firewall: "on", Antivirus: "on", AntivirusProduct: "Microsoft Defender", SecureBoot: "on"},
		Software: []protocol.Software{
			{Name: "7-Zip", Version: "24.08", Publisher: "Igor Pavlov", InstalledAt: &inst},
			{Name: "Firefox", Version: "132.0", Publisher: "Mozilla"},
			{Name: "Git", Version: "2.47", Publisher: "The Git Development Community"},
		},
	}
	inv.Hash = protocol.InventoryHash(inv)
	return inv
}

// enrolledAs inscribe un equipo de la plataforma y arquitectura dadas.
func (a *agentTest) enrolledAs(platform, arch string) *agentClient {
	a.t.Helper()
	a.n++
	req := sampleEnroll(a.token(nil, nil), fmt.Sprintf("hw-as-%d", a.n))
	req.Platform, req.Arch = platform, arch
	resp, out := a.enrollFrom(fmt.Sprintf("203.0.113.%d", 100+a.n%100), req)
	if resp.StatusCode != http.StatusCreated {
		a.t.Fatalf("inscripción: %d", resp.StatusCode)
	}
	return &agentClient{a: a, id: uuid.MustParse(out.DeviceID), authz: "Bearer " + auth.FormatDeviceCredential(out.DeviceID, out.Credential)}
}

func (a *agentTest) setAutoUpdate(on bool) {
	a.t.Helper()
	v := settings.Defaults()
	v.AgentAutoUpdate = on
	if err := settings.Save(ctx, a.store, v, a.admin.ID, time.Now()); err != nil {
		a.t.Fatal(err)
	}
}

// pinDeviceGroup pone al equipo en un grupo con la versión congelada.
func (a *agentTest) pinDeviceGroup(c *agentClient, version string) {
	a.t.Helper()
	g, err := a.store.CreateGroup(ctx, fmt.Sprintf("Congelado %d", a.n), "")
	if err != nil {
		a.t.Fatal(err)
	}
	gid := &g.ID
	if err := a.store.UpdateDevice(ctx, c.id, store.DevicePatch{GroupID: &gid}); err != nil {
		a.t.Fatal(err)
	}
	if err := a.store.SetAgentPin(ctx, g.ID, version); err != nil {
		a.t.Fatal(err)
	}
}
