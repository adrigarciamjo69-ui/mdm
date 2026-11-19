package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	w := newRotatingWriter(path, 100, 3)
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 12; i++ {              // 480 bytes: varias rotaciones
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		fi, _ := e.Info()
		if fi.Size() > 100 {
			t.Errorf("%s supera el tamaño máximo: %d", e.Name(), fi.Size())
		}
	}
	got := strings.Join(names, ",")
	if got != "agent.log,agent.log.1,agent.log.2,agent.log.3" {
		t.Fatalf("ficheros: %s", got)
	}
}

func TestRotatingWriter_AppendsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	w := newRotatingWriter(path, 1000, 3)
	_, _ = w.Write([]byte("uno\n"))
	_ = w.Close()
	w = newRotatingWriter(path, 1000, 3)
	_, _ = w.Write([]byte("dos\n"))
	_ = w.Close()
	if b, _ := os.ReadFile(path); string(b) != "uno\ndos\n" {
		t.Fatalf("contenido: %q", b)
	}
}
