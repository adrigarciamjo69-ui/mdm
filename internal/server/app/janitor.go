package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/alerts"
	"mdm/internal/server/androidsvc"
	"mdm/internal/server/automations"
	"mdm/internal/server/compliance"
	"mdm/internal/server/files"
	"mdm/internal/server/jobs"
	"mdm/internal/server/metrics"
	"mdm/internal/server/notify"
	"mdm/internal/server/reports"
	"mdm/internal/server/store"
)

const (
	// activityRetention es el tiempo que se conserva la actividad por horas.
	activityRetention = 90 * 24 * time.Hour
	// jobRetention es el tiempo que se conservan las tareas terminadas o fallidas.
	jobRetention = 7 * 24 * time.Hour
	// outputRetention es el tiempo que se conserva la salida de las órdenes.
	outputRetention = 30 * 24 * time.Hour
)

// PurgeOnce borra las sesiones caducadas, la actividad antigua y las tareas viejas.
func PurgeOnce(ctx context.Context, s *store.Store, now time.Time) {
	purge := func(what string, f func() (int64, error)) {
		if n, err := f(); err != nil {
			slog.Error("limpieza", "de", what, "err", err)
		} else if n > 0 {
			slog.Info("limpieza", "de", what, "borrados", n)
		}
	}
	purge("sesiones", func() (int64, error) { return s.PurgeExpiredSessions(ctx, now) })
	purge("actividad", func() (int64, error) { return s.PurgeOldActivity(ctx, now.Add(-activityRetention)) })
	purge("tareas", func() (int64, error) { return s.PurgeJobs(ctx, now.Add(-jobRetention)) })
	purge("salida de órdenes", func() (int64, error) { return s.PurgeCommandOutput(ctx, now.Add(-outputRetention)) })
}

// fileRetention es lo que se conserva un instalador que nada usa.
const fileRetention = 7 * 24 * time.Hour

// PurgeFiles borra los instaladores sin uso subidos hace más de 7 días.
func PurgeFiles(ctx context.Context, s *store.Store, fs *files.Store, now time.Time) {
	shas, err := s.UnreferencedFiles(ctx, now.Add(-fileRetention))
	if err != nil {
		slog.Error("limpieza", "de", "ficheros", "err", err)
		return
	}
	for _, sha := range shas {
		if err := fs.Remove(sha); err != nil {
			slog.Error("limpieza: borrar fichero", "sha256", sha, "err", err)
			continue
		}
		_ = s.DeleteFile(ctx, sha)
	}
	if len(shas) > 0 {
		slog.Info("limpieza", "de", "ficheros", "borrados", len(shas))
	}
}

// RegisterJobs registra en la cola las tareas en segundo plano del servidor.
func RegisterJobs(r *jobs.Runner, s *store.Store, fs *files.Store, now func() time.Time) {
	r.Handle("cleanup", func(ctx context.Context, _ json.RawMessage) error {
		PurgeOnce(ctx, s, now())
		if fs != nil {
			PurgeFiles(ctx, s, fs, now())
		}
		return nil
	})
	r.Every("cleanup", time.Hour)

	// groups.recompute: sin carga, todos los grupos (cada hora); con device_id,
	// los grupos de un equipo; con group_id, los miembros de un grupo.
	r.Handle("groups.recompute", func(ctx context.Context, payload json.RawMessage) error {
		var p struct {
			DeviceID string `json:"device_id"`
			GroupID  string `json:"group_id"`
		}
		_ = json.Unmarshal(payload, &p)
		switch {
		case p.DeviceID != "":
			id, err := uuid.Parse(p.DeviceID)
			if err != nil {
				return err
			}
			_, err = s.RecomputeDevice(ctx, id, now())
			return err
		case p.GroupID != "":
			id, err := uuid.Parse(p.GroupID)
			if err != nil {
				return err
			}
			return s.RecomputeGroup(ctx, id, now())
		}
		return s.RecomputeAll(ctx, now())
	})
	r.Every("groups.recompute", time.Hour)

	// compliance.recompute aplica las reglas del servidor (días sin conexión,
	// versiones mínimas) y caduca los informes de más de 24 horas.
	r.Handle("compliance.recompute", func(ctx context.Context, _ json.RawMessage) error {
		changed, err := compliance.RecomputeAll(ctx, s, now())
		for _, id := range changed {
			_ = store.EnqueueRecomputeDevice(ctx, s.Pool(), id, now())
		}
		return err
	})
	r.Every("compliance.recompute", 15*time.Minute)

	r.Handle("commands.expire", func(ctx context.Context, _ json.RawMessage) error {
		_, err := s.ExpireCommands(ctx, now())
		return err
	})
	r.Every("commands.expire", 5*time.Minute)
}

// RegisterAndroidJobs registra la sincronización con Google (cada 15 minutos)
// y el envío de órdenes a los móviles (cada minuto). Sin empresa vinculada no
// hacen nada.
func RegisterAndroidJobs(r *jobs.Runner, svc *androidsvc.Service) {
	r.Handle("android.sync", func(ctx context.Context, _ json.RawMessage) error { return svc.Sync(ctx) })
	r.Every("android.sync", 15*time.Minute)
	r.Handle("android.commands", func(ctx context.Context, _ json.RawMessage) error { return svc.ProcessCommands(ctx) })
	r.Every("android.commands", time.Minute)
}

// EventHandler recibe cada suceso interno.
type EventHandler func(ctx context.Context, e store.Event)

// RegisterAlertJobs registra el reparto de sucesos, la evaluación de alertas
// (cada 5 minutos), la preparación de avisos y las entregas a los canales.
func RegisterAlertJobs(r *jobs.Runner, engine *alerts.Engine, deliverer *notify.Deliverer, handlers ...EventHandler) {
	r.Handle("events.dispatch", func(ctx context.Context, payload json.RawMessage) error {
		var e store.Event
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil // un suceso ilegible no se reintenta
		}
		engine.OnEvent(ctx, e)
		for _, h := range handlers {
			h(ctx, e)
		}
		return nil
	})
	r.Handle("alerts.evaluate", func(ctx context.Context, _ json.RawMessage) error {
		engine.EvaluateState(ctx)
		return nil
	})
	r.Every("alerts.evaluate", 5*time.Minute)
	r.Handle("alerts.notify", engine.HandleNotify)
	r.Handle("notify.deliver", deliverer.Handle)
}

// RegisterReportJobs registra el envío de informes programados (comprueba cada 5 minutos).
func RegisterReportJobs(r *jobs.Runner, sc *reports.Scheduler) {
	r.Handle("reports.run", func(ctx context.Context, _ json.RawMessage) error {
		sc.RunDue(ctx)
		return nil
	})
	r.Every("reports.run", 5*time.Minute)
}

// RegisterAutomationJobs registra las automatizaciones programadas (comprueba
// cada minuto) y la limpieza de su historial (90 días).
func RegisterAutomationJobs(r *jobs.Runner, e *automations.Engine, s *store.Store, now func() time.Time) {
	r.Handle("automations.run", func(ctx context.Context, _ json.RawMessage) error {
		e.RunDue(ctx)
		return nil
	})
	r.Every("automations.run", time.Minute)
	r.Handle("automations.purge", func(ctx context.Context, _ json.RawMessage) error {
		_, err := s.PurgeAutomationRuns(ctx, now().Add(-90*24*time.Hour))
		return err
	})
	r.Every("automations.purge", 24*time.Hour)
}

// RegisterMetricsJobs registra la instantánea diaria (comprueba cada 15 minutos
// si ya pasaron las 00:05 y aún no se tomó la del día).
func RegisterMetricsJobs(r *jobs.Runner, sn *metrics.Snapshotter) {
	r.Handle("metrics.snapshot", func(ctx context.Context, _ json.RawMessage) error {
		_, err := sn.Run(ctx)
		return err
	})
	r.Every("metrics.snapshot", 15*time.Minute)
}
