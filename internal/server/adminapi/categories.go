package adminapi

import (
	"errors"
	"net/http"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type categoryJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Assets int    `json:"assets"`
}

func (a *api) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.d.Store.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]categoryJSON, 0, len(cats))
	for _, c := range cats {
		out = append(out, categoryJSON{ID: c.ID.String(), Name: c.Name, Assets: c.Assets})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) categoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Ya existe una categoría con ese nombre.")
	case errors.Is(err, store.ErrInvalid):
		badRequest(w, "Escribe un nombre para la categoría.")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Esta categoría ya no existe.")
	default:
		a.serverError(w, r, err)
	}
}

func (a *api) createCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	if len([]rune(body.Name)) > 60 {
		badRequest(w, "El nombre no puede tener más de 60 caracteres.")
		return
	}
	c, err := a.d.Store.CreateCategory(r.Context(), body.Name)
	if err != nil {
		a.categoryError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.CategoryCreated, TargetType: "asset_category", TargetID: c.ID.String(),
		Details: map[string]any{"name": c.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, categoryJSON{ID: c.ID.String(), Name: c.Name})
}

func (a *api) renameCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	if len([]rune(body.Name)) > 60 {
		badRequest(w, "El nombre no puede tener más de 60 caracteres.")
		return
	}
	if err := a.d.Store.RenameCategory(r.Context(), id, body.Name); err != nil {
		a.categoryError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.CategoryUpdated, TargetType: "asset_category", TargetID: id.String(),
		Details: map[string]any{"name": body.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, categoryJSON{ID: id.String(), Name: body.Name})
}

func (a *api) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.d.Store.DeleteCategory(r.Context(), id)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Hay activos en esta categoría (también en la papelera): muévelos antes de borrarla.")
		return
	}
	if err != nil {
		a.categoryError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.CategoryDeleted, TargetType: "asset_category", TargetID: id.String(), IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
