package adminapi

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/files"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	// MaxUpload es el tamaño máximo de un instalador.
	MaxUpload     = 4 << 30
	uploadTimeout = 30 * time.Minute
)

// uploadFile recibe un instalador en streaming (multipart, campo «file»).
func (a *api) uploadFile(w http.ResponseWriter, r *http.Request) {
	if a.d.Files == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servidor no tiene configurado el almacenamiento de ficheros.")
		return
	}
	// El plazo general de lectura (60 s) no basta para varios GB.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadTimeout))
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
		sha, size, err := a.d.Files.Put(r.Context(), part, MaxUpload)
		switch {
		case errors.Is(err, files.ErrTooLarge):
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, httpx.CodeTooLarge, "El fichero supera los 4 GB.")
			return
		case errors.Is(err, files.ErrDiskFull):
			httpx.WriteError(w, http.StatusInsufficientStorage, httpx.CodeUnavailable, "No queda espacio en el disco del servidor (se reserva 1 GB libre).")
			return
		case err != nil:
			badRequest(w, "La subida se interrumpió antes de terminar. Vuelve a intentarlo.")
			return
		}
		// El navegador puede enviar la ruta completa (C:\fakepath\…): solo el nombre.
		name := part.FileName()
		name = path.Base(strings.ReplaceAll(name, `\`, "/"))
		name = store.CleanString(name)
		admin := adminFrom(r.Context())
		if err := a.d.Store.SaveFile(r.Context(), store.File{SHA256: sha, Size: size, OriginalName: name,
			ContentType: store.CleanString(part.Header.Get("Content-Type")), UploadedBy: &admin.ID, UploadedAt: a.d.Now().UTC()}); err != nil {
			a.serverError(w, r, err)
			return
		}
		a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.FileUploaded, TargetType: "file", TargetID: sha,
			Details: map[string]any{"name": name, "size": size}, IP: a.ip(r)})
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"sha256": sha, "size": size, "original_name": name})
		return
	}
}
