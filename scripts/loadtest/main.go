// loadtest simula muchos agentes contra un servidor real: los inscribe, los
// mantiene haciendo check-in cada minuto y envía inventario cuando el servidor
// lo pide. Al terminar informa de la latencia del check-in y falla si hay
// errores o si el percentil 99 supera los 200 ms.
//
//	go run ./scripts/loadtest --server https://mdm.ejemplo.es --token <token> --agents 1000 --duration 10m
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"mdm/internal/agent/client"
	"mdm/internal/protocol"
)

const (
	enrollPerSecond = 20
	maxP99          = 200 * time.Millisecond
	checkinEvery    = 60 * time.Second
)

// percentile devuelve el percentil p (0-100) por el método del rango más
// cercano, sin modificar ds.
func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := int(p/100*float64(len(s)) + 0.999999)
	if rank < 1 {
		rank = 1
	}
	if rank > len(s) {
		rank = len(s)
	}
	return s[rank-1]
}

// verdict decide si la prueba se da por buena.
func verdict(p99 time.Duration, errors int) bool { return errors == 0 && p99 <= maxP99 }

// syntheticInventory genera un inventario estable para el agente i: misma
// huella en cada llamada, distinta entre agentes.
func syntheticInventory(i int) protocol.Inventory {
	r := rand.New(rand.NewSource(int64(i)))
	inv := protocol.Inventory{
		CollectedAt: time.Now().UTC(),
		Unavailable: []string{},
		System:      protocol.System{Hostname: fmt.Sprintf("carga-%04d", i), OSName: "Windows 11 Pro", OSVersion: "10.0.26100", Arch: "amd64"},
		Hardware: protocol.Hardware{Manufacturer: "Simulado", Model: "Carga", Serial: fmt.Sprintf("SIM%06d", i),
			CPUModel: "CPU simulada", CPUCores: 8, CPUThreads: 16, MemoryBytes: 16 << 30},
		Disks: []protocol.Disk{{Device: "C:", Mount: "C:", Filesystem: "NTFS", SizeBytes: 512 << 30,
			FreeBytes: uint64(r.Intn(400)+50) << 30, Encrypted: i%10 != 0}},
		Network:  []protocol.NetIf{{Name: "Ethernet", MAC: fmt.Sprintf("02:00:00:%02x:%02x:%02x", i>>16&255, i>>8&255, i&255), IPv4: []string{fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255)}}},
		Security: protocol.Security{DiskEncryption: "on", Firewall: "on", Antivirus: "on", AntivirusProduct: "Microsoft Defender", SecureBoot: "on"},
	}
	for j := 0; j < 150; j++ {
		inv.Software = append(inv.Software, protocol.Software{
			Name: fmt.Sprintf("Programa %03d", j), Version: fmt.Sprintf("%d.%d.%d", j%7+1, r.Intn(20), r.Intn(100)), Publisher: "Editor simulado",
		})
	}
	inv.Hash = protocol.InventoryHash(inv)
	return inv
}

type stats struct {
	mu          sync.Mutex
	latencies   []time.Duration
	errors      atomic.Int64
	enrolled    atomic.Int64
	inventories atomic.Int64
	firstErr    atomic.Value
}

func (s *stats) fail(err error) {
	s.errors.Add(1)
	s.firstErr.CompareAndSwap(nil, err.Error())
}

func main() {
	server := flag.String("server", "", "dirección del servidor (https://…)")
	token := flag.String("token", "", "token de inscripción con usos suficientes para todos los agentes")
	agents := flag.Int("agents", 1000, "número de agentes simulados")
	duration := flag.Duration("duration", 10*time.Minute, "duración de la fase de check-in, tras inscribir a todos")
	insecure := flag.Bool("insecure", false, "no verificar el certificado del servidor (solo para pruebas)")
	flag.Parse()
	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "Uso: loadtest --server https://… --token <token> [--agents 1000] [--duration 10m] [--insecure]")
		os.Exit(2)
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 256
	if *insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opción explícita de pruebas
	}
	hc := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	run := fmt.Sprintf("%x", time.Now().UnixNano())[:8]
	enrollPhase := time.Duration(*agents/enrollPerSecond+1) * time.Second
	deadline := time.Now().Add(enrollPhase + *duration)
	s := &stats{}

	fmt.Printf("Inscribiendo %d agentes (unos %s) y midiendo durante %s…\n", *agents, enrollPhase.Round(time.Second), *duration)
	var wg sync.WaitGroup
	for i := 0; i < *agents; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			simulate(ctx, i, run, *server, *token, hc, deadline, s)
		}(i)
	}

	ticker := time.NewTicker(15 * time.Second)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			s.mu.Lock()
			n := len(s.latencies)
			s.mu.Unlock()
			fmt.Printf("  inscritos %d, check-ins %d, inventarios %d, errores %d\n", s.enrolled.Load(), n, s.inventories.Load(), s.errors.Load())
		}
	}

	s.mu.Lock()
	lat := s.latencies
	s.mu.Unlock()
	p50, p95, p99 := percentile(lat, 50), percentile(lat, 95), percentile(lat, 99)
	fmt.Printf("\nAgentes inscritos:  %d de %d\n", s.enrolled.Load(), *agents)
	fmt.Printf("Check-ins:          %d\n", len(lat))
	fmt.Printf("Inventarios:        %d\n", s.inventories.Load())
	fmt.Printf("Errores:            %d\n", s.errors.Load())
	if e := s.firstErr.Load(); e != nil {
		fmt.Printf("Primer error:       %s\n", e)
	}
	fmt.Printf("Latencia check-in:  p50 %s, p95 %s, p99 %s (límite p99 %s)\n", p50.Round(time.Millisecond/10), p95.Round(time.Millisecond/10), p99.Round(time.Millisecond/10), maxP99)
	if !verdict(p99, int(s.errors.Load())) || len(lat) == 0 {
		fmt.Println("\nResultado: NO APTO")
		os.Exit(1)
	}
	fmt.Println("\nResultado: apto")
}

func simulate(ctx context.Context, i int, run, server, token string, hc *http.Client, deadline time.Time, s *stats) {
	// Inscripciones escalonadas para no llegar todas en el mismo segundo.
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(i) * time.Second / enrollPerSecond):
	}
	c, err := client.New(server, "", hc)
	if err != nil {
		s.fail(err)
		return
	}
	c.Version = "loadtest"
	inv := syntheticInventory(i)
	resp, err := c.Enroll(ctx, protocol.EnrollRequest{
		Token: token, HardwareID: fmt.Sprintf("carga-%s-%04d", run, i), Hostname: inv.System.Hostname,
		Platform: protocol.PlatformWindows, OSName: inv.System.OSName, OSVersion: inv.System.OSVersion, Arch: "amd64",
		Serial: inv.Hardware.Serial, Manufacturer: "Simulado", Model: "Carga", AgentVersion: "loadtest",
	})
	if err != nil {
		s.fail(fmt.Errorf("inscripción: %w", err))
		return
	}
	s.enrolled.Add(1)
	c, _ = client.New(server, resp.DeviceID+"."+resp.Credential, hc)
	c.Version = "loadtest"
	r := rand.New(rand.NewSource(int64(i) + 1))

	for time.Now().Before(deadline) && ctx.Err() == nil {
		start := time.Now()
		out, err := c.Checkin(ctx, protocol.CheckinRequest{AgentVersion: "loadtest", InventoryHash: inv.Hash})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			s.fail(fmt.Errorf("check-in: %w", err))
		} else {
			s.mu.Lock()
			s.latencies = append(s.latencies, time.Since(start))
			s.mu.Unlock()
			if out.SendInventory {
				inv.CollectedAt = time.Now().UTC()
				if err := c.SendInventory(ctx, inv); err != nil {
					s.fail(fmt.Errorf("inventario: %w", err))
				} else {
					s.inventories.Add(1)
				}
			}
		}
		wait := time.Duration(float64(checkinEvery) * (0.9 + 0.2*r.Float64()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
