package patches

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mdm/internal/protocol"
)

var ctx = context.Background()

type fakeScanner struct {
	scans    int
	installs [][]string
	pending  []protocol.UpdateInfo
	reboot   bool
}

func (f *fakeScanner) Scan(context.Context) (protocol.UpdateScan, error) {
	f.scans++
	return protocol.UpdateScan{Updates: f.pending, RebootPending: f.reboot}, nil
}

func (f *fakeScanner) Install(_ context.Context, ids []string, scope string) (protocol.UpdateResults, error) {
	f.installs = append(f.installs, ids)
	res := protocol.UpdateResults{RebootPending: true}
	for _, id := range ids {
		res.Results = append(res.Results, protocol.UpdateResult{ID: id, Status: "installed"})
	}
	f.pending = nil
	return res, nil
}

type fakeAPI struct {
	scans   []protocol.UpdateScan
	results []protocol.UpdateResults
}

func (f *fakeAPI) SendUpdates(_ context.Context, s protocol.UpdateScan) error {
	f.scans = append(f.scans, s)
	return nil
}
func (f *fakeAPI) SendUpdateResults(_ context.Context, r protocol.UpdateResults) error {
	f.results = append(f.results, r)
	return nil
}

type harness struct {
	now     time.Time
	scanner *fakeScanner
	api     *fakeAPI
	powers  []string
	m       *Manager
}

func newHarness(t *testing.T, platform string) *harness {
	h := &harness{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), scanner: &fakeScanner{}, api: &fakeAPI{}}
	h.m = New(Deps{Platform: platform, DeviceID: "dev-1", API: h.api, Scanner: h.scanner,
		Power: func(action string, delay time.Duration, msg string) error {
			h.powers = append(h.powers, fmt.Sprintf("%s %v", action, delay))
			return nil
		},
		Now: func() time.Time { return h.now }, StatePath: filepath.Join(t.TempDir(), "patches.json")})
	return h
}

var nightly = protocol.Window{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "22:00", End: "05:00"}

func TestManager_FirstScanThenDailyAtStableTime(t *testing.T) {
	h := newHarness(t, "linux")
	h.m.Step(ctx)
	if h.scanner.scans != 1 || len(h.api.scans) != 1 {
		t.Fatal("sin búsqueda previa, busca en seguida")
	}
	h.m.Step(ctx)
	if h.scanner.scans != 1 {
		t.Fatal("no vuelve a buscar el mismo día")
	}
	at := scanTime("dev-1")
	if at < 0 || at >= 24*time.Hour || scanTime("dev-1") != at {
		t.Fatalf("hora estable: %v", at)
	}
	next := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).Add(at)
	h.now = next.Add(-time.Minute)
	h.m.Step(ctx)
	if h.scanner.scans != 1 {
		t.Fatal("antes de su hora no busca")
	}
	h.now = next.Add(time.Minute)
	h.m.Step(ctx)
	if h.scanner.scans != 2 {
		t.Fatal("al día siguiente, a su hora, busca")
	}
}

func TestManager_InstallsOnlyInWindow(t *testing.T) {
	h := newHarness(t, "windows")
	h.scanner.pending = []protocol.UpdateInfo{{ID: "u1"}, {ID: "u2"}}
	h.m.SetPolicy(&protocol.PatchPolicy{Window: nightly, Reboot: "never", ApprovedUpdateIDs: []string{"u1"}})
	h.m.Step(ctx)
	if len(h.scanner.installs) != 0 {
		t.Fatal("fuera de la ventana no instala")
	}
	h.now = time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC)
	h.m.Step(ctx)
	if len(h.scanner.installs) != 1 || len(h.scanner.installs[0]) != 1 || h.scanner.installs[0][0] != "u1" {
		t.Fatalf("instala solo las aprobadas que faltan: %v", h.scanner.installs)
	}
	if len(h.api.results) != 1 || len(h.api.scans[len(h.api.scans)-1].Updates) != 0 {
		t.Fatal("informa del resultado y vuelve a buscar (la última búsqueda ya no tiene pendientes)")
	}
	h.m.Step(ctx)
	if len(h.scanner.installs) != 1 {
		t.Fatal("no repite si no queda nada aprobado pendiente")
	}
}

func TestManager_InstallNowIgnoresWindow(t *testing.T) {
	h := newHarness(t, "linux")
	h.scanner.pending = []protocol.UpdateInfo{{ID: "openssl", Security: true}}
	h.m.SetPolicy(&protocol.PatchPolicy{Window: nightly, Reboot: "never", LinuxScope: "security"})
	h.m.InstallNow()
	h.m.Step(ctx)
	if len(h.scanner.installs) != 1 {
		t.Fatal("«Instalar ahora» no espera a la ventana")
	}
}

func TestManager_RebootPolicy(t *testing.T) {
	for _, tc := range []struct {
		reboot string
		at     time.Time
		now    bool
		want   int
	}{
		{"never", time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC), false, 0},
		{"in_window", time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC), false, 1},
		{"notify", time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC), false, 1},
		{"in_window", time.Date(2026, 11, 17, 11, 0, 0, 0, time.UTC), true, 0}, // instalar ahora fuera de la ventana: no reinicia
		{"notify", time.Date(2026, 11, 17, 11, 0, 0, 0, time.UTC), true, 1},
	} {
		h := newHarness(t, "windows")
		h.now = tc.at
		h.scanner.pending = []protocol.UpdateInfo{{ID: "u1"}}
		h.m.SetPolicy(&protocol.PatchPolicy{Window: nightly, Reboot: tc.reboot, NotifyMinutes: 15, ApprovedUpdateIDs: []string{"u1"}})
		if tc.now {
			h.m.InstallNow()
		}
		h.m.Step(ctx)
		if len(h.powers) != tc.want {
			t.Errorf("%s a las %s: reinicios %v", tc.reboot, tc.at.Format("15:04"), h.powers)
		}
		if tc.want == 1 && h.powers[0] != "reboot 15m0s" {
			t.Errorf("aviso previo: %v", h.powers)
		}
	}
}

func TestManager_ScanNow(t *testing.T) {
	h := newHarness(t, "linux")
	h.m.Step(ctx)
	h.m.ScanNow()
	h.m.Step(ctx)
	if h.scanner.scans != 2 {
		t.Fatal("«Buscar ahora» fuerza una búsqueda")
	}
}
