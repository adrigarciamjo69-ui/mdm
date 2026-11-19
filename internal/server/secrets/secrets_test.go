package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func key(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func TestBox_RoundTrip(t *testing.T) {
	b, err := New(key(t))
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := b.Seal([]byte("hola"))
	s2, _ := b.Seal([]byte("hola"))
	if !strings.HasPrefix(s1, "v1:") || s1 == s2 {
		t.Fatalf("formato o nonce: %s %s", s1, s2)
	}
	got, err := b.Open(s1)
	if err != nil || string(got) != "hola" {
		t.Fatalf("Open: %q %v", got, err)
	}
}

func TestNew_RejectsBadKeys(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	long := base64.StdEncoding.EncodeToString(make([]byte, 33))
	for _, k := range []string{"", "no-es-base64!!", short, long} {
		if _, err := New(k); err == nil || !strings.Contains(err.Error(), "openssl rand -base64 32") {
			t.Errorf("%q: %v", k, err)
		}
	}
}

func TestOpen_WrongKey(t *testing.T) {
	b1, _ := New(key(t))
	b2, _ := New(key(t))
	s, _ := b1.Seal([]byte("x"))
	if _, err := b2.Open(s); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("got %v", err)
	}
}

func TestOpen_Malformed(t *testing.T) {
	b, _ := New(key(t))
	for _, s := range []string{"", "v2:AAAA", "v1:!!", "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 10))} {
		if _, err := b.Open(s); !errors.Is(err, ErrUndecryptable) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestOpen_Tampered(t *testing.T) {
	b, _ := New(key(t))
	s, _ := b.Seal([]byte("secreto"))
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "v1:"))
	raw[len(raw)-1] ^= 1
	if _, err := b.Open("v1:" + base64.StdEncoding.EncodeToString(raw)); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("got %v", err)
	}
}
