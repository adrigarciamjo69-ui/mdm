package agentapi

import (
	"context"
	"errors"
	"net/http"

	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type ctxKey int

const keyDevice ctxKey = iota

// deviceFrom devuelve el dispositivo autenticado de la petición.
func deviceFrom(ctx context.Context) store.Device {
	d, _ := ctx.Value(keyDevice).(store.Device)
	return d
}

// requireDevice exige la credencial de un dispositivo no revocado. Todas las
// causas de rechazo responden igual, para no revelar qué identificadores existen.
func (a *api) requireDevice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deny := func() {
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Credencial de dispositivo no válida o revocada.")
		}
		id, secret, err := auth.ParseDeviceCredential(r.Header.Get("Authorization"))
		if err != nil {
			deny()
			return
		}
		dev, err := a.d.Store.DeviceByID(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			deny()
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if dev.RevokedAt != nil || !auth.SecretMatches(dev.CredentialHash, secret) {
			deny()
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyDevice, dev)))
	})
}
