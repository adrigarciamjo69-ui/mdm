package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

// pattern genera n bytes deterministas sin tenerlos en memoria.
type pattern struct{ n, off int64 }

func (p *pattern) Read(b []byte) (int, error) {
	if p.off >= p.n {
		return 0, io.EOF
	}
	k := int64(len(b))
	if rest := p.n - p.off; k > rest {
		k = rest
	}
	for i := int64(0); i < k; i++ {
		b[i] = byte((p.off + i) % 251)
	}
	p.off += k
	return int(k), nil
}

func sumOf(r io.Reader) string {
	h := sha256.New()
	_, _ = io.Copy(h, r)
	return hex.EncodeToString(h.Sum(nil))
}

func newStore(t *testing.T) (*Store, string) {
	dir := t.TempDir()
	s := New(dir, 0)
	return s, dir
}

func entries(t *testing.T, dir string) []string {
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestPut_StreamsAndHashes(t *testing.T) {
	s, dir := newStore(t)
	const size = 300 << 20
	want := sumOf(&pattern{n: size})
	var peak atomic.Uint64
	stop := make(chan struct{})
	go func() {
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapInuse > peak.Load() {
					peak.Store(m.HeapInuse)
				}
			}
		}
	}()
	sha, n, err := s.Put(ctx, &pattern{n: size}, 4<<30)
	close(stop)
	if err != nil || sha != want || n != size {
		t.Fatalf("Put: %s %d %v", sha, n, err)
	}
	if peak.Load() > 50<<20 {
		t.Fatalf("la subida no debe cargarse en memoria: pico %d MB", peak.Load()>>20)
	}
	if _, err := os.Stat(filepath.Join(dir, sha[:2], sha)); err != nil {
		t.Fatal("el fichero no está en <dir>/<sha[0:2]>/<sha>")
	}
	f, info, err := s.Open(sha)
	if err != nil || info.Size() != size {
		t.Fatalf("Open: %v", err)
	}
	f.Close()
}

type failing struct {
	r   io.Reader
	at  int64
	got int64
}

func (f *failing) Read(b []byte) (int, error) {
	if f.got >= f.at {
		return 0, errors.New("conexión cortada")
	}
	n, err := f.r.Read(b)
	f.got += int64(n)
	return n, err
}

func TestPut_InterruptedLeavesNothing(t *testing.T) {
	s, dir := newStore(t)
	_, _, err := s.Put(ctx, &failing{r: &pattern{n: 10 << 20}, at: 3 << 20}, 4<<30)
	if err == nil {
		t.Fatal("una subida cortada debe fallar")
	}
	if e := entries(t, dir); len(e) != 0 {
		t.Fatalf("quedan ficheros a medias: %v", e)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.Put(c, &pattern{n: 1 << 20}, 4<<30); err == nil {
		t.Fatal("un contexto cancelado debe abortar la subida")
	}
	if e := entries(t, dir); len(e) != 0 {
		t.Fatalf("quedan ficheros tras cancelar: %v", e)
	}
}

func TestPut_DedupesSameContent(t *testing.T) {
	s, dir := newStore(t)
	a, _, _ := s.Put(ctx, bytes.NewReader([]byte("instalador")), 1<<20)
	b, _, _ := s.Put(ctx, bytes.NewReader([]byte("instalador")), 1<<20)
	if a != b || len(entries(t, dir)) != 1 {
		t.Fatal("el mismo contenido debe guardarse una vez")
	}
}

func TestPut_TooLargeAndDiskFull(t *testing.T) {
	s, dir := newStore(t)
	if _, _, err := s.Put(ctx, &pattern{n: 2 << 20}, 1<<20); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("demasiado grande: %v", err)
	}
	if len(entries(t, dir)) != 0 {
		t.Fatal("no debe quedar nada tras rechazar por tamaño")
	}
	full := New(dir, 1<<62)
	if _, _, err := full.Put(ctx, bytes.NewReader([]byte("x")), 1<<20); !errors.Is(err, ErrDiskFull) {
		t.Fatalf("disco lleno: %v", err)
	}
}

func TestRemoveAndBadSHA(t *testing.T) {
	s, _ := newStore(t)
	sha, _, _ := s.Put(ctx, bytes.NewReader([]byte("x")), 10)
	if err := s.Remove(sha); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(sha); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tras borrar: %v", err)
	}
	if _, _, err := s.Open("../../etc/passwd"); err == nil {
		t.Fatal("un sha inválido no debe abrir nada")
	}
}
