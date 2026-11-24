package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

type CheckinParams struct {
	AgentVersion string
	IP           string
	At           time.Time
	// Capabilities, si no es nula, sustituye las capacidades guardadas del equipo.
	Capabilities []string
}

// RecordCheckin anota la conexión del agente y suma uno al contador de
// actividad de la hora en curso.
func (s *Store) RecordCheckin(ctx context.Context, id uuid.UUID, p CheckinParams) error {
	_, err := s.pool.Exec(ctx,
		`WITH u AS (
		     UPDATE devices SET last_seen_at = $2, last_ip = $3, agent_version = $4,
		            capabilities = COALESCE($6::text[], capabilities) WHERE id = $1 RETURNING id
		 )
		 INSERT INTO device_activity (device_id, hour, checkins)
		 SELECT id, $5, 1 FROM u
		 ON CONFLICT (device_id, hour) DO UPDATE SET checkins = device_activity.checkins + 1`,
		id, p.At, p.IP, p.AgentVersion, p.At.UTC().Truncate(time.Hour), p.Capabilities)
	if err != nil {
		return fmt.Errorf("registrar check-in: %w", err)
	}
	return nil
}

type ActivityPoint struct {
	Hour     time.Time
	Checkins int
}

// DeviceActivity devuelve los check-ins por hora desde since, en orden ascendente.
func (s *Store) DeviceActivity(ctx context.Context, id uuid.UUID, since time.Time) ([]ActivityPoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT hour, checkins FROM device_activity WHERE device_id = $1 AND hour >= $2 ORDER BY hour`, id, since)
	if err != nil {
		return nil, fmt.Errorf("leer actividad: %w", err)
	}
	defer rows.Close()
	out := []ActivityPoint{}
	for rows.Next() {
		var p ActivityPoint
		if err := rows.Scan(&p.Hour, &p.Checkins); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// inventoryData es lo que se guarda en device_inventory.data: todo el
// inventario menos el software, que vive en su propia tabla.
type inventoryData struct {
	Unavailable []string          `json:"unavailable"`
	System      protocol.System   `json:"system"`
	Hardware    protocol.Hardware `json:"hardware"`
	Disks       []protocol.Disk   `json:"disks"`
	Network     []protocol.NetIf  `json:"network"`
	Security    protocol.Security `json:"security"`
	Mobile      *protocol.Mobile  `json:"mobile,omitempty"`
}

// SaveInventory sustituye el inventario del dispositivo en una transacción.
// Las secciones que el agente marca como no disponibles conservan su valor
// anterior en lugar de vaciarse. El inventario debe llegar ya saneado
// (SanitizeInventory).
func (s *Store) SaveInventory(ctx context.Context, id uuid.UUID, inv protocol.Inventory, at time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// El bloqueo serializa dos envíos simultáneos del mismo equipo.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM devices WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	missing := map[string]bool{}
	for _, sec := range inv.Unavailable {
		missing[sec] = true
	}
	data := inventoryData{Unavailable: inv.Unavailable, System: inv.System, Hardware: inv.Hardware,
		Disks: inv.Disks, Network: inv.Network, Security: inv.Security, Mobile: inv.Mobile}
	if data.Unavailable == nil {
		data.Unavailable = []string{}
	}
	if data.Disks == nil {
		data.Disks = []protocol.Disk{}
	}
	if data.Network == nil {
		data.Network = []protocol.NetIf{}
	}

	if len(missing) > 0 {
		var prevRaw []byte
		err := tx.QueryRow(ctx, `SELECT data FROM device_inventory WHERE device_id = $1`, id).Scan(&prevRaw)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			var prev inventoryData
			if err := json.Unmarshal(prevRaw, &prev); err != nil {
				return fmt.Errorf("leer inventario anterior: %w", err)
			}
			if missing[protocol.SectionSystem] {
				data.System = prev.System
			}
			if missing[protocol.SectionHardware] {
				data.Hardware = prev.Hardware
			}
			if missing[protocol.SectionDisks] && prev.Disks != nil {
				data.Disks = prev.Disks
			}
			if missing[protocol.SectionNetwork] && prev.Network != nil {
				data.Network = prev.Network
			}
			if missing[protocol.SectionSecurity] {
				data.Security = prev.Security
			}
		}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if !missing[protocol.SectionHardware] {
		if err := applyChassis(ctx, tx, id, inv.Hardware.Chassis); err != nil {
			return fmt.Errorf("categoría del activo: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO device_inventory (device_id, collected_at, data) VALUES ($1, $2, $3)
		 ON CONFLICT (device_id) DO UPDATE SET collected_at = EXCLUDED.collected_at, data = EXCLUDED.data`,
		id, inv.CollectedAt, raw); err != nil {
		return fmt.Errorf("guardar inventario: %w", err)
	}

	if !missing[protocol.SectionSoftware] {
		if _, err := tx.Exec(ctx, `DELETE FROM device_software WHERE device_id = $1`, id); err != nil {
			return err
		}
		rows := make([][]any, 0, len(inv.Software))
		for _, sw := range inv.Software {
			rows = append(rows, []any{id, sw.Name, sw.Version, sw.Publisher, sw.InstalledAt})
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"device_software"},
			[]string{"device_id", "name", "version", "publisher", "installed_at"}, pgx.CopyFromRows(rows)); err != nil {
			return fmt.Errorf("guardar software: %w", err)
		}
	}

	// Los campos del listado siguen al inventario, pero una sección no
	// disponible o un valor vacío nunca borran lo que ya se sabía.
	sys, hw := inv.System, inv.Hardware
	if missing[protocol.SectionSystem] {
		sys = protocol.System{}
	}
	if missing[protocol.SectionHardware] {
		hw = protocol.Hardware{}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE devices SET inventory_hash = $2, inventory_at = $3,
		     hostname = COALESCE(NULLIF($4, ''), hostname),
		     os_name = COALESCE(NULLIF($5, ''), os_name),
		     os_version = COALESCE(NULLIF($6, ''), os_version),
		     serial = COALESCE(NULLIF($7, ''), serial),
		     manufacturer = COALESCE(NULLIF($8, ''), manufacturer),
		     model = COALESCE(NULLIF($9, ''), model)
		 WHERE id = $1`,
		id, inv.Hash, at, sys.Hostname, sys.OSName, sys.OSVersion, hw.Serial, hw.Manufacturer, hw.Model); err != nil {
		return fmt.Errorf("actualizar dispositivo: %w", err)
	}
	return tx.Commit(ctx)
}

// DeviceInventory devuelve el inventario guardado (sin software) y la fecha en
// que el agente lo recogió. ErrNotFound si el equipo aún no ha enviado ninguno.
func (s *Store) DeviceInventory(ctx context.Context, id uuid.UUID) (json.RawMessage, time.Time, error) {
	var raw []byte
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT data, collected_at FROM device_inventory WHERE device_id = $1`, id).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("leer inventario: %w", err)
	}
	return raw, at, nil
}

// ListSoftware devuelve una página del software del dispositivo, ordenado por
// nombre, y el total. q filtra por nombre sin distinguir mayúsculas.
func (s *Store) ListSoftware(ctx context.Context, id uuid.UUID, q string, page, perPage int) ([]protocol.Software, int, error) {
	page, perPage = normalizePage(page, perPage)
	pattern := "%" + escapeLike(q) + "%"
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM device_software WHERE device_id = $1 AND name ILIKE $2`, id, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar software: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT name, version, publisher, installed_at FROM device_software
		 WHERE device_id = $1 AND name ILIKE $2
		 ORDER BY lower(name), version LIMIT $3 OFFSET $4`, id, pattern, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("listar software: %w", err)
	}
	defer rows.Close()
	out := []protocol.Software{}
	for rows.Next() {
		var sw protocol.Software
		if err := rows.Scan(&sw.Name, &sw.Version, &sw.Publisher, &sw.InstalledAt); err != nil {
			return nil, 0, err
		}
		out = append(out, sw)
	}
	return out, total, rows.Err()
}

// PurgeOldActivity borra la actividad anterior a before.
func (s *Store) PurgeOldActivity(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM device_activity WHERE hour < $1`, before)
	return tag.RowsAffected(), err
}
