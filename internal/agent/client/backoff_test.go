package client

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func TestBackoff_Sequence(t *testing.T) {
	b := NewBackoff(func() float64 { return 0.5 })
	want := []time.Duration{5, 10, 20, 40, 80, 160, 320, 640, 900, 900}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("paso %d: %v, se esperaba %v", i, got, w*time.Second)
		}
	}
}

func TestBackoff_Jitter(t *testing.T) {
	if got := NewBackoff(func() float64 { return 0 }).Next(); got != 4*time.Second {
		t.Errorf("rnd=0: %v", got)
	}
	if got := NewBackoff(func() float64 { return 1 }).Next(); got != 6*time.Second {
		t.Errorf("rnd=1: %v", got)
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := NewBackoff(func() float64 { return 0.5 })
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("tras Reset: %v", got)
	}
}
