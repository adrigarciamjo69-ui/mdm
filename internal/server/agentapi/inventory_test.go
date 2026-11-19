package agentapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mdm/internal/protocol"
)

type storedInventory struct {
	Unavailable []string            `json:"unavailable"`
	System      protocol.System     `json:"system"`
	Hardware    protocol.Hardware   `json:"hardware"`
	Disks       []protocol.Disk     `json:"disks"`
	Network     []protocol.NetIf    `json:"network"`
	Security    protocol.Security   `json:"security"`
	Software    []protocol.Software `json:"software"`
}

func (c *agentClient) stored() (storedInventory, string) {
	c.a.t.Helper()
	raw, _, err := c.a.store.DeviceInventory(ctx, c.id)
	if err != nil {
		c.a.t.Fatalf("leer inventario: %v", err)
	}
	var s storedInventory
	if err := json.Unmarshal(raw, &s); err != nil {
		c.a.t.Fatal(err)
	}
	return s, string(raw)
}

func (c *agentClient) software() []protocol.Software {
	c.a.t.Helper()
	sw, _, err := c.a.store.ListSoftware(ctx, c.id, "", 1, 200)
	if err != nil {
		c.a.t.Fatal(err)
	}
	return sw
}

func TestInventory_StoresSnapshotAndSoftware(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	if resp := c.inventory(inv); resp.StatusCode != 204 {
		t.Fatalf("estado %d: %s", resp.StatusCode, bodyString(resp))
	}
	s, raw := c.stored()
	if strings.Contains(raw, `"software"`) {
		t.Error("el software no debe guardarse dentro de data")
	}
	if s.System.OSVersion != "10.0.26200" || s.Hardware.Model != "ThinkPad T14" || len(s.Disks) != 1 ||
		len(s.Network) != 1 || s.Security.AntivirusProduct != "Microsoft Defender" {
		t.Errorf("instantánea: %s", raw)
	}
	sw := c.software()
	if len(sw) != 3 || sw[0].Name != "7-Zip" || sw[0].InstalledAt == nil || sw[1].Publisher != "Mozilla" {
		t.Errorf("software: %+v", sw)
	}
	d := a.device(c.id.String())
	if d.InventoryHash != inv.Hash || d.InventoryAt == nil || !d.InventoryAt.Equal(a.clock.Now()) {
		t.Errorf("huella o fecha: %s %v", d.InventoryHash, d.InventoryAt)
	}
	if d.Hostname != "PC-NUEVO" || d.OSName != "Windows 11 Pro" || d.OSVersion != "10.0.26200" ||
		d.Serial != "PF123" || d.Manufacturer != "Lenovo" || d.Model != "ThinkPad T14" {
		t.Errorf("los campos del listado no reflejan el inventario: %+v", d)
	}
}

func TestInventory_ReplacesPrevious(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	c.inventory(sampleInventory())
	inv := sampleInventory()
	inv.Software = inv.Software[:1]
	inv.Disks = append(inv.Disks, protocol.Disk{Device: "D:", SizeBytes: 1 << 30})
	c.inventory(inv)
	if sw := c.software(); len(sw) != 1 {
		t.Fatalf("software tras el segundo envío: %+v", sw)
	}
	if s, _ := c.stored(); len(s.Disks) != 2 {
		t.Fatalf("discos tras el segundo envío: %+v", s.Disks)
	}
}

func TestInventory_UnavailableSectionKeepsPrevious(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	c.inventory(sampleInventory())

	partial := sampleInventory()
	partial.Unavailable = []string{"software", "security"}
	partial.Software = nil
	partial.Security = protocol.Security{}
	partial.System.OSVersion = "10.0.26300"
	if resp := c.inventory(partial); resp.StatusCode != 204 {
		t.Fatalf("estado %d", resp.StatusCode)
	}
	if sw := c.software(); len(sw) != 3 {
		t.Errorf("el software anterior se perdió: %+v", sw)
	}
	s, _ := c.stored()
	if s.Security.AntivirusProduct != "Microsoft Defender" || s.Security.Firewall != "on" {
		t.Errorf("la seguridad anterior se perdió: %+v", s.Security)
	}
	if s.System.OSVersion != "10.0.26300" {
		t.Errorf("las secciones disponibles no se actualizaron: %+v", s.System)
	}
	if len(s.Unavailable) != 2 {
		t.Errorf("unavailable no se guardó: %v", s.Unavailable)
	}
}

func TestInventory_UnavailableSystemDoesNotBlankListFields(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	inv.Unavailable = []string{"system", "hardware"}
	inv.System, inv.Hardware = protocol.System{}, protocol.Hardware{}
	c.inventory(inv)
	d := a.device(c.id.String())
	if d.Hostname != "PC-A" || d.OSName != "Windows 11 Pro" || d.Serial != "SN123" {
		t.Fatalf("los campos del listado se vaciaron: %+v", d)
	}
}

func rawInventory(a *agentTest, authz string, body []byte, gz bool) *http.Response {
	r := httptest.NewRequest("POST", "/inventory", bytes.NewReader(body))
	r.RemoteAddr = "203.0.113.50:40000"
	r.Header.Set("Authorization", authz)
	if gz {
		r.Header.Set("Content-Encoding", "gzip")
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w.Result()
}

func gz(b []byte, level int) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, level)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

func TestInventory_RequiresGzip(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	plain, _ := json.Marshal(sampleInventory())
	wantError(t, rawInventory(a, c.authz, plain, false), 400, "invalid_request")
	wantError(t, rawInventory(a, c.authz, []byte("esto no es gzip"), true), 400, "invalid_request")
	wantError(t, rawInventory(a, c.authz, gz([]byte("{no json"), gzip.BestSpeed), true), 400, "invalid_request")
}

func TestInventory_CompressedTooLarge(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	// Datos incompresibles: más de 1 MB incluso comprimidos.
	junk := make([]byte, 1100*1024)
	x := uint32(1)
	for i := range junk {
		x = x*1664525 + 1013904223
		junk[i] = byte(x >> 24)
	}
	wantError(t, rawInventory(a, c.authz, gz(junk, gzip.NoCompression), true), 413, "too_large")
}

func TestInventory_DecompressedTooLarge(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	bomb := gz(bytes.Repeat([]byte{' '}, 9<<20), gzip.BestCompression)
	if len(bomb) > 1<<20 {
		t.Fatalf("la bomba no cabe comprimida: %d", len(bomb))
	}
	wantError(t, rawInventory(a, c.authz, bomb, true), 413, "too_large")
}

func TestInventory_NulBytesAndHugeStringsSanitized(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	inv.Software = []protocol.Software{
		{Name: "Evil\x00App", Version: "1", Publisher: strings.Repeat("p", 5000)},
		{Name: "\x00", Version: "sin nombre tras sanear"},
	}
	inv.System.Hostname = "PC\x00-X"
	inv.Security.Firewall = "quizá"
	inv.Unavailable = []string{"inventada"}
	if resp := c.inventory(inv); resp.StatusCode != 204 {
		t.Fatalf("estado %d: %s", resp.StatusCode, bodyString(resp))
	}
	sw := c.software()
	if len(sw) != 1 || sw[0].Name != "EvilApp" || len([]rune(sw[0].Publisher)) != 512 {
		t.Fatalf("software saneado: %+v", sw)
	}
	s, _ := c.stored()
	if s.System.Hostname != "PC-X" || s.Security.Firewall != "unknown" || len(s.Unavailable) != 0 {
		t.Fatalf("instantánea saneada: %+v %+v %v", s.System, s.Security, s.Unavailable)
	}
	if d := a.device(c.id.String()); d.Hostname != "PC-X" {
		t.Fatalf("hostname del listado: %q", d.Hostname)
	}
}

func TestInventory_ConcurrentSendsBothSucceed(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = c.inventory(sampleInventory()).StatusCode
		}(i)
	}
	wg.Wait()
	for _, code := range codes {
		if code != 204 {
			t.Fatalf("códigos: %v", codes)
		}
	}
	if sw := c.software(); len(sw) != 3 {
		t.Fatalf("filas de software duplicadas o perdidas: %d", len(sw))
	}
}

func TestInventory_EmptyListsStoredAsArrays(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	inv := sampleInventory()
	inv.Disks, inv.Network, inv.Unavailable = nil, nil, nil
	c.inventory(inv)
	_, raw := c.stored()
	for _, want := range []string{`"disks": []`, `"network": []`, `"unavailable": []`} {
		if !strings.Contains(raw, want) {
			t.Errorf("falta %s en %s", want, raw)
		}
	}
}
