package adminapi

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdm/internal/server/auth"
)

// upload envía un multipart con el campo «file» por la sesión del cliente.
func (c *testClient) upload(name string, body io.Reader) *http.Response {
	c.api.t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, _ := mw.CreateFormFile("file", name)
		if _, err := io.Copy(part, body); err != nil {
			pw.CloseWithError(err)
			return
		}
		mw.Close()
		pw.Close()
	}()
	r := httptest.NewRequest("POST", "/files", pr)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	c.authorize(r)
	w := httptest.NewRecorder()
	c.api.handler.ServeHTTP(w, r)
	return w.Result()
}

func TestUploadAPI_AdminOnlyAndAudited(t *testing.T) {
	a := newTestAPI(t)
	if resp := a.loginAs(auth.RoleOperator).upload("app.msi", strings.NewReader("x")); resp.StatusCode != 403 {
		t.Fatalf("operador: %d", resp.StatusCode)
	}
	resp := a.loginAs(auth.RoleAdmin).upload(`C:\fakepath\Contabilidad 2026.msi`, strings.NewReader("contenido del instalador"))
	wantStatus(t, resp, 201)
	got := decode[struct {
		SHA256       string `json:"sha256"`
		Size         int64  `json:"size"`
		OriginalName string `json:"original_name"`
	}](t, resp)
	if len(got.SHA256) != 64 || got.Size != 24 || got.OriginalName != "Contabilidad 2026.msi" {
		t.Fatalf("subida: %+v", got)
	}
	f, err := a.store.GetFile(ctx, got.SHA256)
	if err != nil || f.Size != 24 {
		t.Fatalf("fila: %+v %v", f, err)
	}
	if e := a.findAudit("file.uploaded"); e == nil || e.TargetID != got.SHA256 {
		t.Fatalf("auditoría: %+v", e)
	}
}

type brokenBody struct{ n int }

func (b *brokenBody) Read(p []byte) (int, error) {
	if b.n > 3 {
		return 0, errors.New("cliente desconectado")
	}
	b.n++
	return copy(p, bytes.Repeat([]byte("x"), len(p))), nil
}

func TestUploadAPI_ClientDisconnect(t *testing.T) {
	a := newTestAPI(t)
	resp := a.loginAs(auth.RoleAdmin).upload("grande.msi", &brokenBody{})
	if resp.StatusCode < 400 {
		t.Fatalf("una subida cortada no puede dar éxito: %d", resp.StatusCode)
	}
	if n, _ := a.store.CountFiles(ctx); n != 0 {
		t.Fatalf("no debe quedar ninguna fila: %d", n)
	}
}

func TestUploadAPI_NotMultipart(t *testing.T) {
	a := newTestAPI(t)
	wantError(t, a.loginAs(auth.RoleAdmin).do("POST", "/files", map[string]string{"x": "y"}), 400, "invalid_request")
}
