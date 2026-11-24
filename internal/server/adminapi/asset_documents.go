package adminapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// maxDocument es el tamaño máximo de un documento de un activo.
const maxDocument = 20 << 20

// docTypes son las extensiones admitidas, su tipo y la firma con la que debe
// empezar el fichero (vacía = texto sin firma).
var docTypes = map[string]struct {
	mime   string
	magics [][]byte
}{
	".pdf":  {"application/pdf", [][]byte{[]byte("%PDF-")}},
	".doc":  {"application/msword", [][]byte{{0xD0, 0xCF, 0x11, 0xE0}}},
	".xls":  {"application/vnd.ms-excel", [][]byte{{0xD0, 0xCF, 0x11, 0xE0}}},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", [][]byte{[]byte("PK\x03\x04")}},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", [][]byte{[]byte("PK\x03\x04")}},
	".zip":  {"application/zip", [][]byte{[]byte("PK\x03\x04"), []byte("PK\x05\x06")}},
	".jpg":  {"image/jpeg", [][]byte{{0xFF, 0xD8, 0xFF}}},
	".jpeg": {"image/jpeg", [][]byte{{0xFF, 0xD8, 0xFF}}},
	".png":  {"image/png", [][]byte{[]byte("\x89PNG\r\n\x1a\n")}},
	".gif":  {"image/gif", [][]byte{[]byte("GIF87a"), []byte("GIF89a")}},
	".webp": {"image/webp", [][]byte{[]byte("RIFF")}},
	".txt":  {"text/plain; charset=utf-8", nil},
}

type documentJSON struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	UploadedBy  string `json:"uploaded_by"`
	UploadedAt  string `json:"uploaded_at"`
}

func docToJSON(d store.AssetDocument) documentJSON {
	return documentJSON{ID: d.ID.String(), Filename: d.Filename, ContentType: d.ContentType, Size: d.Size,
		UploadedBy: d.UploaderEmail, UploadedAt: d.UploadedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

func (a *api) listAssetDocuments(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return
	}
	docs, err := a.d.Store.ListAssetDocuments(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]documentJSON, 0, len(docs))
	for _, d := range docs {
		out = append(out, docToJSON(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) uploadAssetDocument(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return
	}
	if a.d.Files == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servidor no tiene configurado el almacenamiento de ficheros.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocument+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, "Envía el fichero como multipart/form-data en el campo «file».")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			badRequest(w, "No se ha recibido ningún fichero en el campo «file».")
			return
		}
		if part.FormName() != "file" {
			continue
		}
		name := store.CleanString(path.Base(strings.ReplaceAll(part.FileName(), `\`, "/")))
		ext := strings.ToLower(path.Ext(name))
		typ, known := docTypes[ext]
		if !known || name == "" {
			badRequest(w, "Tipo de fichero no admitido. Se admiten PDF, Word, Excel, imágenes (JPEG, PNG, GIF, WebP), texto y ZIP.")
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, maxDocument+1))
		if err != nil {
			badRequest(w, "La subida se interrumpió antes de terminar. Vuelve a intentarlo.")
			return
		}
		if len(data) > maxDocument {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, httpx.CodeTooLarge, "El fichero supera los 20 MB.")
			return
		}
		if len(data) == 0 || !matchesMagic(data, typ.magics) {
			badRequest(w, "El contenido del fichero no corresponde a su extensión ("+ext+").")
			return
		}
		sha, size, err := a.d.Files.Put(r.Context(), bytes.NewReader(data), maxDocument)
		if err != nil {
			httpx.WriteError(w, http.StatusInsufficientStorage, httpx.CodeUnavailable, "No se ha podido guardar el fichero: puede que no quede espacio en el disco del servidor.")
			return
		}
		admin := adminFrom(r.Context())
		now := a.d.Now().UTC()
		if err := a.d.Store.SaveFile(r.Context(), store.File{SHA256: sha, Size: size, OriginalName: name, ContentType: typ.mime,
			UploadedBy: &admin.ID, UploadedAt: now}); err != nil {
			a.serverError(w, r, err)
			return
		}
		doc, err := a.d.Store.AddAssetDocument(r.Context(), store.AssetDocument{AssetID: row.ID, SHA256: sha, Filename: name,
			ContentType: typ.mime, Size: size, UploadedBy: &admin.ID, UploadedAt: now})
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		doc.UploaderEmail = admin.Email
		a.auditAsset(r, audit.AssetDocumentAdded, row.ID, map[string]any{"filename": name, "size": size})
		httpx.WriteJSON(w, http.StatusCreated, docToJSON(doc))
		return
	}
}

func matchesMagic(data []byte, magics [][]byte) bool {
	if magics == nil {
		// Texto: sin bytes nulos.
		return !bytes.Contains(data, []byte{0})
	}
	for _, m := range magics {
		if bytes.HasPrefix(data, m) {
			return true
		}
	}
	return false
}

func (a *api) loadDocument(w http.ResponseWriter, r *http.Request) (store.AssetDocument, bool) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return store.AssetDocument{}, false
	}
	docID, err := uuid.Parse(chi.URLParam(r, "doc"))
	if err != nil {
		notFound(w, "Este documento ya no existe.")
		return store.AssetDocument{}, false
	}
	d, err := a.d.Store.AssetDocument(r.Context(), row.ID, docID)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este documento ya no existe.")
		return store.AssetDocument{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.AssetDocument{}, false
	}
	return d, true
}

func (a *api) downloadAssetDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDocument(w, r)
	if !ok {
		return
	}
	f, info, err := a.d.Files.Open(d.SHA256)
	if err != nil {
		notFound(w, "El fichero ya no está en el servidor.")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", d.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Filename}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	_, _ = io.Copy(w, f)
}

func (a *api) deleteAssetDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDocument(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteAssetDocument(r.Context(), d.AssetID, d.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetDocumentDeleted, d.AssetID, map[string]any{"filename": d.Filename})
	w.WriteHeader(http.StatusNoContent)
}
