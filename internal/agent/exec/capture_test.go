package exec

import (
	"bytes"
	"strings"
	"testing"
)

func TestCapture_KeepsTail(t *testing.T) {
	c := newCapture(64 * 1024)
	chunk := bytes.Repeat([]byte("x"), 1024*1024)
	for i := 0; i < 50; i++ {
		if n, err := c.Write(chunk); err != nil || n != len(chunk) {
			t.Fatal("Write debe aceptar todo lo escrito")
		}
	}
	_, _ = c.Write([]byte("FINAL"))
	out := c.String()
	if !strings.HasPrefix(out, "[salida recortada]\n") || !strings.HasSuffix(out, "FINAL") || len(out) > 64*1024+len("[salida recortada]\n") {
		t.Fatalf("longitud %d, prefijo %q", len(out), out[:20])
	}
	if cap(c.buf) > 2*64*1024 {
		t.Fatalf("la memoria debe estar acotada: %d", cap(c.buf))
	}
}

func TestCapture_Small(t *testing.T) {
	c := newCapture(100)
	_, _ = c.Write([]byte("hola"))
	if c.String() != "hola" {
		t.Fatalf("%q", c.String())
	}
}
