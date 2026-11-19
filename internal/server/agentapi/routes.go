package agentapi

import "github.com/go-chi/chi/v5"

// routes registra las rutas que exigen credencial de dispositivo.
func (a *api) routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireDevice)
		r.Post("/checkin", a.checkin)
		r.Post("/inventory", a.inventory)
		r.Get("/policy", a.getPolicy)
		r.Post("/compliance", a.postCompliance)
		r.Post("/recovery-keys", a.postRecoveryKey)
		r.Get("/files/{sha256}", a.downloadFile)
		r.Post("/apps/status", a.postAppStatus)
		r.Post("/updates", a.postUpdates)
		r.Post("/updates/results", a.postUpdateResults)
		r.Post("/commands/{id}/start", a.startCommand)
		r.Post("/commands/{id}/result", a.finishCommand)
	})
}
