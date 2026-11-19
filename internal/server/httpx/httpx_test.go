package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientIP_TrustedProxyUsesLastForwarded(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.2:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := ClientIP(r, true); got != "203.0.113.9" {
		t.Fatalf("got %s", got)
	}
}

func TestClientIP_UntrustedIgnoresHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.2:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := ClientIP(r, false); got != "172.18.0.2" {
		t.Fatalf("got %s", got)
	}
}

func TestClientIP_TrustedWithoutHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.2:5555"
	if got := ClientIP(r, true); got != "172.18.0.2" {
		t.Fatalf("got %s", got)
	}
}

func TestClientIP_TrustedGarbageHeaderFallsBack(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "[2001:db8::1]:443"
	r.Header.Set("X-Forwarded-For", "no-es-una-ip")
	if got := ClientIP(r, true); got != "2001:db8::1" {
		t.Fatalf("got %s", got)
	}
}

func TestDecodeJSON_TooLarge(t *testing.T) {
	body := `{"a":"` + strings.Repeat("x", 17*1024) + `"}`
	w := httptest.NewRecorder()
	var dst struct{ A string }
	ok := DecodeJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), 16*1024, &dst)
	if ok || w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"too_large"`) {
		t.Fatalf("ok=%v code=%d body=%s", ok, w.Code, w.Body)
	}
}

func TestDecodeJSON_Malformed(t *testing.T) {
	for _, body := range []string{"{", "", `{"a":1} sobra`, `{"desconocido":1}`} {
		w := httptest.NewRecorder()
		var dst struct {
			A int `json:"a"`
		}
		ok := DecodeJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), 1024, &dst)
		if ok || w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
			t.Errorf("%q: ok=%v code=%d body=%s", body, ok, w.Code, w.Body)
		}
	}
}

func TestDecodeJSON_OK(t *testing.T) {
	w := httptest.NewRecorder()
	var dst struct {
		A int `json:"a"`
	}
	if !DecodeJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"a":7}`)), 1024, &dst) || dst.A != 7 {
		t.Fatalf("no decodificó: %+v %s", dst, w.Body)
	}
}

func TestWriteError_Shape(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusNotFound, "not_found", "No existe.")
	want := `{"error":{"code":"not_found","message":"No existe."}}`
	if strings.TrimSpace(w.Body.String()) != want || w.Code != 404 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("content-type %s", ct)
	}
}
