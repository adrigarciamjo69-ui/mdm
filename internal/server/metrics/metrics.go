// Package metrics toma la instantánea diaria del parque para las gráficas de
// tendencia del resumen.
package metrics

import (
	"context"
	"time"

	"mdm/internal/server/store"
)

// Keys son las series que guarda la instantánea.
var Keys = []string{"devices.total", "devices.online", "devices.offline", "devices.inactive", "devices.windows", "devices.linux", "devices.android",
	"compliance.percent", "patches.security_pending", "deployments.failed", "alerts.open.critical", "alerts.open.warning", "alerts.open.info"}

type Snapshotter struct {
	Store            *store.Store
	Now              func() time.Time
	Location         func() *time.Location
	CheckinIntervalS int
}

// Run toma la instantánea del día si son más de las 00:05 (en la zona de las
// programaciones) y aún no se ha tomado. Devuelve si la ha tomado.
func (sn *Snapshotter) Run(ctx context.Context) (bool, error) {
	now := sn.Now().In(sn.Location())
	if now.Hour() == 0 && now.Minute() < 5 {
		return false, nil
	}
	day := now.Format("2006-01-02")
	if done, err := sn.Store.HasMetrics(ctx, day); err != nil || done {
		return false, err
	}
	sum, err := sn.Store.Summary(ctx, now, sn.CheckinIntervalS)
	if err != nil {
		return false, err
	}
	alerts, err := sn.Store.OpenAlertCounts(ctx)
	if err != nil {
		return false, err
	}
	pct, err := sn.Store.CompliancePercent(ctx)
	if err != nil {
		return false, err
	}
	v := map[string]float64{
		"devices.total": float64(sum.Total), "devices.online": float64(sum.ByStatus[store.StatusOnline]),
		"devices.offline": float64(sum.ByStatus[store.StatusOffline]), "devices.inactive": float64(sum.ByStatus[store.StatusInactive]),
		"devices.windows": float64(sum.ByPlatform["windows"]), "devices.linux": float64(sum.ByPlatform["linux"]), "devices.android": float64(sum.ByPlatform["android"]),
		"patches.security_pending": float64(sum.SecurityUpdatesPending), "deployments.failed": float64(sum.DeploymentFailures),
		"alerts.open.critical": float64(alerts["critical"]), "alerts.open.warning": float64(alerts["warning"]), "alerts.open.info": float64(alerts["info"]),
	}
	if pct >= 0 {
		v["compliance.percent"] = pct
	}
	return true, sn.Store.SaveMetrics(ctx, day, v)
}
