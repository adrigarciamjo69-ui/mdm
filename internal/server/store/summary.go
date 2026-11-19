package store

import (
	"context"
	"fmt"
	"time"
)

type OSCount struct {
	Platform  string
	OSName    string
	OSVersion string
	Count     int
}

type Summary struct {
	Total      int
	ByStatus   map[Status]int
	ByPlatform map[string]int
	// OSVersions lleva las 10 combinaciones de sistema y versión más frecuentes.
	OSVersions []OSCount
	// LowDisk, Unencrypted y Stale cuentan solo equipos no revocados.
	LowDisk, Unencrypted, Stale int
	RecentEnrollments           []DeviceRow
	// Compliance cuenta los equipos no revocados por estado de cumplimiento.
	Compliance map[string]int
	// SecurityUpdatesPending cuenta los equipos no revocados con alguna actualización de seguridad pendiente.
	SecurityUpdatesPending int
	// DeploymentFailures cuenta los equipos no revocados con algún despliegue fallido.
	DeploymentFailures int
}

// Summary calcula los datos de la pantalla de resumen.
func (s *Store) Summary(ctx context.Context, now time.Time, checkinIntervalS int) (Summary, error) {
	c := cutoffs(now, checkinIntervalS)
	out := Summary{ByStatus: map[Status]int{}, ByPlatform: map[string]int{}, OSVersions: []OSCount{}}

	var online, offline, inactive, revoked, windows, linux, android int
	cut := onlineCutoffSQL("$1", "$4")
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*),
		        count(*) FILTER (WHERE d.revoked_at IS NULL AND d.last_seen_at > `+cut+`),
		        count(*) FILTER (WHERE d.revoked_at IS NULL AND (d.last_seen_at IS NULL OR (d.last_seen_at <= `+cut+` AND d.last_seen_at > $2))),
		        count(*) FILTER (WHERE d.revoked_at IS NULL AND d.last_seen_at <= $2),
		        count(*) FILTER (WHERE d.revoked_at IS NOT NULL),
		        count(*) FILTER (WHERE d.platform = 'windows'),
		        count(*) FILTER (WHERE d.platform = 'linux'),
		        count(*) FILTER (WHERE d.platform = 'android'),
		        count(*) FILTER (WHERE %s),
		        count(*) FILTER (WHERE %s),
		        count(*) FILTER (WHERE d.revoked_at IS NULL AND COALESCE(d.last_seen_at, d.enrolled_at) <= $3)
		 FROM devices d`, fmt.Sprintf(sqlLowDisk, lowDiskRatio), sqlUnencrypted),
		c.online, c.inactive, c.stale, c.mobileOnline).Scan(&out.Total, &online, &offline, &inactive, &revoked, &windows, &linux, &android,
		&out.LowDisk, &out.Unencrypted, &out.Stale)
	if err != nil {
		return Summary{}, fmt.Errorf("calcular resumen: %w", err)
	}
	out.ByStatus[StatusOnline], out.ByStatus[StatusOffline] = online, offline
	out.ByStatus[StatusInactive], out.ByStatus[StatusRevoked] = inactive, revoked
	out.ByPlatform["windows"], out.ByPlatform["linux"] = windows, linux
	if android > 0 {
		out.ByPlatform["android"] = android
	}

	rows, err := s.pool.Query(ctx,
		`SELECT platform, os_name, os_version, count(*) AS n FROM devices
		 GROUP BY platform, os_name, os_version ORDER BY n DESC, platform, os_name, os_version LIMIT 10`)
	if err != nil {
		return Summary{}, fmt.Errorf("contar versiones de sistema: %w", err)
	}
	for rows.Next() {
		var o OSCount
		if err := rows.Scan(&o.Platform, &o.OSName, &o.OSVersion, &o.Count); err != nil {
			rows.Close()
			return Summary{}, err
		}
		out.OSVersions = append(out.OSVersions, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}

	out.Compliance = map[string]int{"compliant": 0, "noncompliant": 0, "unknown": 0}
	crow, err := s.pool.Query(ctx,
		`SELECT COALESCE(c.state, 'unknown'), count(*) FROM devices d
		 LEFT JOIN device_compliance c ON c.device_id = d.id
		 WHERE d.revoked_at IS NULL GROUP BY 1`)
	if err != nil {
		return Summary{}, err
	}
	for crow.Next() {
		var st string
		var n int
		if err := crow.Scan(&st, &n); err != nil {
			crow.Close()
			return Summary{}, err
		}
		out.Compliance[st] = n
	}
	crow.Close()

	if err := s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT d.device_id) FROM device_updates d JOIN updates u ON u.platform = d.platform AND u.id = d.update_id
		 JOIN devices v ON v.id = d.device_id WHERE v.revoked_at IS NULL AND u.security AND d.state IN ('missing', 'failed')`).Scan(&out.SecurityUpdatesPending); err != nil {
		return Summary{}, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT st.device_id) FROM deployment_status st JOIN devices v ON v.id = st.device_id
		 WHERE v.revoked_at IS NULL AND st.state = 'failed'`).Scan(&out.DeploymentFailures); err != nil {
		return Summary{}, err
	}

	recent, _, err := s.ListDevices(ctx, DeviceFilter{Sort: "enrolled_at", Order: "desc", Page: 1, PerPage: 10,
		Now: now, CheckinIntervalS: checkinIntervalS})
	if err != nil {
		return Summary{}, err
	}
	out.RecentEnrollments = recent
	return out, nil
}
