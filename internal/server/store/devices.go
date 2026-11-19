package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

// ErrTokenInvalid indica que el token de inscripción no existe, ha caducado,
// está revocado o ha agotado sus usos. No se distingue el motivo a propósito.
var ErrTokenInvalid = errors.New("token de inscripción no válido")

const deviceCols = `d.id, d.hardware_id, d.hostname, d.platform, d.os_name, d.os_version, d.arch, d.serial,
	d.manufacturer, d.model, d.agent_version, d.credential_hash, d.revoked_at, d.group_id, d.assigned_user,
	d.tags, d.enrolled_at, d.last_seen_at, d.last_ip, d.inventory_hash, d.inventory_at, d.capabilities`

func deviceDest(d *Device) []any {
	return []any{&d.ID, &d.HardwareID, &d.Hostname, &d.Platform, &d.OSName, &d.OSVersion, &d.Arch, &d.Serial,
		&d.Manufacturer, &d.Model, &d.AgentVersion, &d.CredentialHash, &d.RevokedAt, &d.GroupID, &d.AssignedUser,
		&d.Tags, &d.EnrolledAt, &d.LastSeenAt, &d.LastIP, &d.InventoryHash, &d.InventoryAt, &d.Capabilities}
}

type EnrollParams struct {
	TokenHash      string
	CredentialHash string
	Req            protocol.EnrollRequest
	IP             string
	At             time.Time
}

type EnrollResult struct {
	DeviceID uuid.UUID
	// Reenrolled indica que el equipo ya existía (mismo hardware_id).
	Reenrolled       bool
	PreviousHostname string
	TokenID          uuid.UUID
	TokenName        string
}

// EnrollDevice consume un uso del token y crea el dispositivo, o lo reinscribe
// si su hardware_id ya existe. Todo ocurre en una transacción: si algo falla,
// el uso del token no se consume.
func (s *Store) EnrollDevice(ctx context.Context, p EnrollParams) (EnrollResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EnrollResult{}, err
	}
	defer tx.Rollback(ctx)

	var res EnrollResult
	var groupID *uuid.UUID
	// Una sola sentencia comprueba y consume: el bloqueo de fila hace que dos
	// inscripciones simultáneas nunca superen max_uses.
	err = tx.QueryRow(ctx,
		`UPDATE enrollment_tokens SET uses = uses + 1
		 WHERE token_hash = $1 AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > $2)
		   AND (max_uses IS NULL OR uses < max_uses)
		 RETURNING id, name, group_id`, p.TokenHash, p.At).Scan(&res.TokenID, &res.TokenName, &groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrollResult{}, ErrTokenInvalid
	}
	if err != nil {
		return EnrollResult{}, fmt.Errorf("consumir token: %w", err)
	}

	r := p.Req
	// El nombre anterior se lee antes de sobrescribirlo; el bloqueo serializa
	// dos reinscripciones simultáneas del mismo equipo. En una reinscripción
	// se conservan grupo, usuario, etiquetas y fecha de alta.
	err = tx.QueryRow(ctx, `SELECT hostname FROM devices WHERE hardware_id = $1 FOR UPDATE`, r.HardwareID).
		Scan(&res.PreviousHostname)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EnrollResult{}, fmt.Errorf("buscar dispositivo: %w", err)
	}
	err = tx.QueryRow(ctx,
		` INSERT INTO devices AS d (hardware_id, hostname, platform, os_name, os_version, arch, serial,
		                      manufacturer, model, agent_version, credential_hash, group_id, enrolled_at, last_ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (hardware_id) DO UPDATE SET
		     hostname = EXCLUDED.hostname, platform = EXCLUDED.platform, os_name = EXCLUDED.os_name,
		     os_version = EXCLUDED.os_version, arch = EXCLUDED.arch, serial = EXCLUDED.serial,
		     manufacturer = EXCLUDED.manufacturer, model = EXCLUDED.model,
		     agent_version = EXCLUDED.agent_version, credential_hash = EXCLUDED.credential_hash,
		     last_ip = EXCLUDED.last_ip, revoked_at = NULL
		 RETURNING d.id, (xmax <> 0)`,
		r.HardwareID, r.Hostname, r.Platform, r.OSName, r.OSVersion, r.Arch, r.Serial,
		r.Manufacturer, r.Model, r.AgentVersion, p.CredentialHash, groupID, p.At, p.IP).
		Scan(&res.DeviceID, &res.Reenrolled)
	if err != nil {
		return EnrollResult{}, fmt.Errorf("guardar dispositivo: %w", err)
	}
	typ := EventDeviceEnrolled
	if res.Reenrolled {
		typ = EventDeviceReenrolled
	}
	if err := emit(ctx, tx, Event{Type: typ, DeviceID: devicePtr(res.DeviceID), At: p.At,
		Data: map[string]any{"hostname": p.Req.Hostname, "platform": p.Req.Platform}}); err != nil {
		return EnrollResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EnrollResult{}, err
	}
	return res, nil
}

func (s *Store) DeviceByID(ctx context.Context, id uuid.UUID) (Device, error) {
	var d Device
	err := s.pool.QueryRow(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.id = $1`, id).Scan(deviceDest(&d)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("leer dispositivo: %w", err)
	}
	return d, nil
}

func (s *Store) CountDevices(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM devices`).Scan(&n)
	return n, err
}

// DevicePatch lleva los cambios opcionales que un administrador puede hacer
// sobre un dispositivo. GroupID distingue «no tocar» (nil) de «quitar el
// grupo» (puntero a nil).
type DevicePatch struct {
	GroupID      **uuid.UUID
	AssignedUser *string
	Tags         *[]string
}

func (s *Store) UpdateDevice(ctx context.Context, id uuid.UUID, p DevicePatch) error {
	setGroup := p.GroupID != nil
	var group *uuid.UUID
	if setGroup {
		group = *p.GroupID
	}
	var tags []string
	if p.Tags != nil {
		tags = *p.Tags
		if tags == nil {
			tags = []string{}
		}
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE devices SET
		     group_id = CASE WHEN $2 THEN $3 ELSE group_id END,
		     assigned_user = COALESCE($4, assigned_user),
		     tags = CASE WHEN $5 THEN $6 ELSE tags END
		 WHERE id = $1`, id, setGroup, group, p.AssignedUser, p.Tags != nil, tags)
	if isForeignKeyViolation(err) {
		return ErrBadReference
	}
	if err != nil {
		return fmt.Errorf("actualizar dispositivo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeDevice invalida la credencial del dispositivo. Revocar uno ya
// revocado no cambia nada.
func (s *Store) RevokeDevice(ctx context.Context, id uuid.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE devices SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("revocar dispositivo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice borra el dispositivo y, en cascada, su inventario, software,
// actividad y órdenes.
func (s *Store) DeleteDevice(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("borrar dispositivo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
