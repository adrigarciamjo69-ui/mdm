package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// server sirve content con ServeContent; si cut > 0, la primera petición
// completa se corta tras cut bytes. ignoreRange hace que no admita rangos.
func server(t *testing.T, content []byte, cut int, ignoreRange bool) (*httptest.Server, *atomic.Int32, *[]string) {
	var calls atomic.Int32
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		ranges = append(ranges, r.Header.Get("Range"))
		if r.Header.Get("Authorization") != "Bearer secreto" {
			w.WriteHeader(401)
			return
		}
		if ignoreRange {
			r.Header.Del("Range")
		}
		if n == 1 && cut > 0 {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(200)
			_, _ = w.Write(content[:cut])
			if hj, ok := w.(http.Hijacker); ok { // cortar la conexión a mitad
				c, _, _ := hj.Hijack()
				c.Close()
			}
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &ranges
}

func getter(srv *httptest.Server) Getter {
	return Getter{Base: srv.URL, Bearer: "secreto", HTTP: srv.Client(), Free: func(string) (uint64, error) { return 1 << 40, nil }}
}

func TestGet_Fresh(t *testing.T) {
	content := bytes.Repeat([]byte("instalador "), 1000)
	srv, _, _ := server(t, content, 0, false)
	dst := filepath.Join(t.TempDir(), "app.msi")
	if err := getter(srv).Get(ctx, sum(content), int64(len(content)), dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, content) {
		t.Fatal("contenido distinto")
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("no debe quedar el .part")
	}
}

func TestGet_ResumesWithRange(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 5000)
	srv, calls, ranges := server(t, content, 20000, false)
	dst := filepath.Join(t.TempDir(), "app.deb")
	g := getter(srv)
	if err := g.Get(ctx, sum(content), int64(len(content)), dst); err == nil {
		t.Fatal("el primer intento cortado debe fallar")
	}
	if info, err := os.Stat(dst + ".part"); err != nil || info.Size() == 0 {
		t.Fatal("debe conservarse lo descargado para reanudar")
	}
	if err := g.Get(ctx, sum(content), int64(len(content)), dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, content) || calls.Load() != 2 || !strings.HasPrefix((*ranges)[1], "bytes=") {
		t.Fatalf("reanudación: %d llamadas, rangos %v", calls.Load(), *ranges)
	}
}

func TestGet_ServerIgnoresRange(t *testing.T) {
	content := bytes.Repeat([]byte("abc"), 3000)
	srv, _, _ := server(t, content, 0, true)
	dst := filepath.Join(t.TempDir(), "x")
	_ = os.WriteFile(dst+".part", content[:100], 0o600) // resto de un intento anterior
	if err := getter(srv).Get(ctx, sum(content), int64(len(content)), dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, content) {
		t.Fatal("si el servidor ignora el rango hay que empezar de cero")
	}
}

func TestGet_BadChecksumDeletes(t *testing.T) {
	content := []byte("manipulado")
	srv, _, _ := server(t, content, 0, false)
	dst := filepath.Join(t.TempDir(), "x")
	err := getter(srv).Get(ctx, strings.Repeat("a", 64), int64(len(content)), dst)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("%v", err)
	}
	for _, p := range []string{dst, dst + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("debe borrarse %s", p)
		}
	}
}

func TestGet_NoSpace(t *testing.T) {
	srv, calls, _ := server(t, []byte("x"), 0, false)
	g := getter(srv)
	g.Free = func(string) (uint64, error) { return 500 << 20, nil }
	err := g.Get(ctx, strings.Repeat("a", 64), 1<<30, filepath.Join(t.TempDir(), "x"))
	if !errors.Is(err, ErrNoSpace) || !strings.Contains(err.Error(), "500 MB") || calls.Load() != 0 {
		t.Fatalf("sin espacio no se descarga y el mensaje lo dice: %v", err)
	}
}
