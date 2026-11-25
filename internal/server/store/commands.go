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

const (
	// MaxBatchDevices es el máximo de equipos de un lote.
	MaxBatchDevices = 1000
	// MaxOutputBytes es el máximo de salida que se guarda por flujo.
	MaxOutputBytes   = 64 * 1024
	defaultCmdExpiry = 7 * 24 * time.Hour
	// noAnswerGrace es lo que se espera tras el tiempo límite de una orden en
	// curso antes de darla por fallida.
	noAnswerGrace      = 10 * time.Minute
	defaultCmdTimeoutS = 600
	truncatedMark      = "[salida recortada]\n"
)

var ErrTooManyDevices = errors.New("demasiados equipos en el lote")

type NewBatch struct {
	Type              string
	Params            json.RawMessage
	CreatedBy         uuid.UUID
	CreatedByEmail    string
	TargetDescription string
	At                time.Time
	ExpiresIn         time.Duration
}

type Batch struct {
	ID                uuid.UUID
	Type              string
	Params            json.RawMessage
	TargetDescription string
	DeviceCount       int
	CreatedByEmail    string
	CreatedAt         time.Time
	CommandIDs        []uuid.UUID
	Counts            map[string]int
}

type Command struct {
	ID         uuid.UUID
	DeviceID   uuid.UUID
	Hostname   string
	BatchID    *uuid.UUID
	Type       string
	Payload    json.RawMessage
	Status     string
	CreatedAt  time.Time
	SentAt     *time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	ExpiresAt  time.Time
	ExitCode   *int
	Stdout     string
	Stderr     string
	Error      string
	// Changed indica, tras FinishCommand, que el resultado se aplicó (no era repetido).
	Changed bool
}

// CleanOutput sanea la salida de una orden y conserva como mucho los últimos
// 64 KB, anteponiendo un aviso si recorta.
func CleanOutput(s string) string {
	s = CleanText(s, len(s)+1)
	if len(s) <= MaxOutputBytes {
		return s
	}
	cut := len(s) - MaxOutputBytes
	for cut < len(s) && (s[cut]&0xC0) == 0x80 { // no partir un carácter
		cut++
	}
	return truncatedMark + s[cut:]
}

func payloadJSON(p json.RawMessage) []byte {
	if len(p) == 0 || string(p) == "null" {
		return []byte(`{}`)
	}
	return p
}

// CreateBatch crea un lote con una orden por equipo.
func (s *Store) CreateBatch(ctx context.Context, b NewBatch, deviceIDs []uuid.UUID) (Batch, error) {
	if len(deviceIDs) > MaxBatchDevices {
		return Batch{}, ErrTooManyDevices
	}
	if b.ExpiresIn <= 0 {
		b.ExpiresIn = defaultCmdExpiry
	}
	out := Batch{Type: b.Type, Params: payloadJSON(b.Params), TargetDescription: b.TargetDescription, DeviceCount: len(deviceIDs),
		CreatedByEmail: b.CreatedByEmail, CreatedAt: b.At}
	var by *uuid.UUID
	if b.CreatedBy != uuid.Nil {
		by = &b.CreatedBy
	}
	err := s.Tx(ctx, func(q Querier) error {
		if err := q.QueryRow(ctx,
			`INSERT INTO command_batches (type, params, target_description, device_count, created_by, created_by_email, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			b.Type, out.Params, b.TargetDescription, len(deviceIDs), by, b.CreatedByEmail, b.At).Scan(&out.ID); err != nil {
			return err
		}
		for _, d := range deviceIDs {
			var id uuid.UUID
			if err := q.QueryRow(ctx,
				`INSERT INTO commands (device_id, batch_id, type, payload, status, created_at, created_by, expires_at)
				 VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7) RETURNING id`,
				d, out.ID, b.Type, out.Params, b.At, by, b.At.Add(b.ExpiresIn)).Scan(&id); err != nil {
				if isForeignKeyViolation(err) {
					return ErrBadReference
				}
				return err
			}
			out.CommandIDs = append(out.CommandIDs, id)
		}
		return nil
	})
	return out, err
}

// requiredCapsSQL calcula la capacidad necesaria para cada tipo de orden; debe
// coincidir con protocol.RequiredCapability.
const requiredCapsSQL = `CASE WHEN type = 'run_script' THEN 'scripts.v1' WHEN type IN ('temp_admin', 'temp_admin_revoke') THEN 'tempadmin.v1' ELSE 'commands.v1' END`

// NextCommands devuelve las órdenes que el agente puede ejecutar, de más
// antigua a más reciente, y marca como enviadas las pendientes. Las enviadas
// y no iniciadas se vuelven a entregar hasta que el agente las confirme.
func (s *Store) NextCommands(ctx context.Context, deviceID uuid.UUID, caps []string, now time.Time, limit int) ([]protocol.Command, error) {
	if caps == nil {
		caps = []string{}
	}
	rows, err := s.pool.Query(ctx,
		`UPDATE commands SET status = 'sent', sent_at = COALESCE(sent_at, $3)
		 WHERE id IN (
		     SELECT id FROM commands
		     WHERE device_id = $1 AND status IN ('pending', 'sent') AND expires_at > $3
		       AND `+requiredCapsSQL+` = ANY($2)
		     ORDER BY created_at, id LIMIT $4)
		 RETURNING id, type, payload, expires_at, created_at`, deviceID, caps, now, limit)
	if err != nil {
		return nil, fmt.Errorf("leer órdenes: %w", err)
	}
	defer rows.Close()
	type item struct {
		cmd     protocol.Command
		created time.Time
	}
	var items []item
	for rows.Next() {
		var it item
		var id uuid.UUID
		if err := rows.Scan(&id, &it.cmd.Type, &it.cmd.Payload, &it.cmd.ExpiresAt, &it.created); err != nil {
			return nil, err
		}
		it.cmd.ID = id.String()
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// RETURNING no garantiza orden: se ordena aquí.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].created.Before(items[j-1].created); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	out := make([]protocol.Command, 0, len(items))
	for _, it := range items {
		out = append(out, it.cmd)
	}
	return out, nil
}

// StartCommand marca la orden en curso. Es idempotente; una orden de otro
// equipo no existe a estos efectos.
func (s *Store) StartCommand(ctx context.Context, deviceID, cmdID uuid.UUID, at time.Time) error {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`WITH upd AS (
		     UPDATE commands SET status = 'running', started_at = $3
		     WHERE id = $1 AND device_id = $2 AND status IN ('pending', 'sent') RETURNING 1)
		 SELECT EXISTS (SELECT 1 FROM commands WHERE id = $1 AND device_id = $2)`, cmdID, deviceID, at).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// FinishCommand guarda el resultado. Un resultado repetido no cambia nada.
// Si una orden «retire» termina bien, el equipo queda revocado.
func (s *Store) FinishCommand(ctx context.Context, deviceID, cmdID uuid.UUID, r protocol.CommandResult, at time.Time) (Command, error) {
	if r.Status != protocol.CommandSucceeded && r.Status != protocol.CommandFailed {
		return Command{}, fmt.Errorf("estado final no válido: %q", r.Status)
	}
	var c Command
	err := s.Tx(ctx, func(q Querier) error {
		var status, typ string
		err := q.QueryRow(ctx, `SELECT status, type FROM commands WHERE id = $1 AND device_id = $2 FOR UPDATE`, cmdID, deviceID).Scan(&status, &typ)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		switch status {
		case protocol.CommandSucceeded, protocol.CommandFailed, protocol.CommandCancelled, protocol.CommandExpired:
			return nil // ya terminada: se ignora
		}
		if _, err := q.Exec(ctx,
			`UPDATE commands SET status = $3, exit_code = $4, stdout = $5, stderr = $6, error = $7,
			     started_at = COALESCE(started_at, $8), finished_at = $8
			 WHERE id = $1 AND device_id = $2`,
			cmdID, deviceID, r.Status, r.ExitCode, CleanOutput(r.Stdout), CleanOutput(r.Stderr), CleanText(r.Error, 2000), at); err != nil {
			return err
		}
		if typ == protocol.CmdRetire && r.Status == protocol.CommandSucceeded {
			if _, err := q.Exec(ctx, `UPDATE devices SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, deviceID, at); err != nil {
				return err
			}
		}
		if r.Status == protocol.CommandFailed {
			if err := emitCommandFailed(ctx, q, cmdID, deviceID, typ, r.Error, at); err != nil {
				return err
			}
		}
		c.Changed = true
		return nil
	})
	if err != nil {
		return Command{}, err
	}
	full, err := s.GetCommand(ctx, cmdID)
	full.Changed = c.Changed
	return full, err
}

// ExpireCommands caduca las órdenes no entregadas a tiempo y da por fallidas
// las que llevan en curso más de su tiempo límite más un margen.
func (s *Store) ExpireCommands(ctx context.Context, now time.Time) (int64, error) {
	t1, err := s.pool.Exec(ctx,
		`UPDATE commands SET status = 'expired', finished_at = $1 WHERE status IN ('pending', 'sent') AND expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	t2, err := s.pool.Exec(ctx,
		`UPDATE commands SET status = 'failed', finished_at = $1, error = 'Sin respuesta del equipo.'
		 WHERE status = 'running' AND started_at
		       + make_interval(secs => COALESCE(NULLIF(payload->>'timeout_s', '')::int, $2)) + $3::interval < $1`,
		now, defaultCmdTimeoutS, fmt.Sprintf("%d seconds", int(noAnswerGrace.Seconds())))
	if err != nil {
		return 0, err
	}
	return t1.RowsAffected() + t2.RowsAffected(), nil
}

// CancelCommand cancela una orden que aún no ha empezado.
func (s *Store) CancelCommand(ctx context.Context, id uuid.UUID, at time.Time) error {
	var status string
	err := s.pool.QueryRow(ctx,
		`WITH upd AS (UPDATE commands SET status = 'cancelled', finished_at = $2 WHERE id = $1 AND status IN ('pending', 'sent') RETURNING status)
		 SELECT COALESCE((SELECT status FROM upd), (SELECT status FROM commands WHERE id = $1), '')`, id, at).Scan(&status)
	if err != nil {
		return err
	}
	switch status {
	case "":
		return ErrNotFound
	case protocol.CommandCancelled:
		return nil
	}
	return ErrConflict
}

const commandCols = `c.id, c.device_id, COALESCE(d.hostname, ''), c.batch_id, c.type, c.payload, c.status, c.created_at, c.sent_at,
	c.started_at, c.finished_at, c.expires_at, c.exit_code, c.stdout, c.stderr, c.error`

func scanCommand(row pgx.Row) (Command, error) {
	var c Command
	err := row.Scan(&c.ID, &c.DeviceID, &c.Hostname, &c.BatchID, &c.Type, &c.Payload, &c.Status, &c.CreatedAt, &c.SentAt,
		&c.StartedAt, &c.FinishedAt, &c.ExpiresAt, &c.ExitCode, &c.Stdout, &c.Stderr, &c.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) GetCommand(ctx context.Context, id uuid.UUID) (Command, error) {
	return scanCommand(s.pool.QueryRow(ctx, `SELECT `+commandCols+` FROM commands c LEFT JOIN devices d ON d.id = c.device_id WHERE c.id = $1`, id))
}

func (s *Store) queryCommands(ctx context.Context, where string, args ...any) ([]Command, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+commandCols+` FROM commands c LEFT JOIN devices d ON d.id = c.device_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		// Los listados no llevan la salida completa.
		c.Stdout, c.Stderr = "", ""
		out = append(out, c)
	}
	return out, rows.Err()
}

// BatchCommands devuelve las órdenes de un lote, por nombre de equipo.
func (s *Store) BatchCommands(ctx context.Context, batchID uuid.UUID) ([]Command, error) {
	return s.queryCommands(ctx, `c.batch_id = $1 ORDER BY lower(COALESCE(d.hostname, '')), c.id`, batchID)
}

// DeviceCommands devuelve las últimas órdenes de un equipo.
func (s *Store) DeviceCommands(ctx context.Context, deviceID uuid.UUID, limit int) ([]Command, error) {
	return s.queryCommands(ctx, `c.device_id = $1 ORDER BY c.created_at DESC, c.id LIMIT $2`, deviceID, limit)
}

func (s *Store) batchCounts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT batch_id, status, count(*) FROM commands WHERE batch_id = ANY($1) GROUP BY 1, 2`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]map[string]int{}
	for rows.Next() {
		var id uuid.UUID
		var st string
		var n int
		if err := rows.Scan(&id, &st, &n); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]int{}
		}
		out[id][st] = n
	}
	return out, rows.Err()
}

const batchCols = `id, type, params, target_description, device_count, created_by_email, created_at`

func scanBatch(row pgx.Row) (Batch, error) {
	var b Batch
	err := row.Scan(&b.ID, &b.Type, &b.Params, &b.TargetDescription, &b.DeviceCount, &b.CreatedByEmail, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

func (s *Store) GetBatch(ctx context.Context, id uuid.UUID) (Batch, error) {
	b, err := scanBatch(s.pool.QueryRow(ctx, `SELECT `+batchCols+` FROM command_batches WHERE id = $1`, id))
	if err != nil {
		return b, err
	}
	counts, err := s.batchCounts(ctx, []uuid.UUID{id})
	b.Counts = withAllStatuses(counts[id])
	return b, err
}

func withAllStatuses(m map[string]int) map[string]int {
	out := map[string]int{}
	for _, st := range []string{"pending", "sent", "running", "succeeded", "failed", "expired", "cancelled"} {
		out[st] = m[st]
	}
	return out
}

// ListBatches devuelve una página de lotes, de más reciente a más antiguo.
func (s *Store) ListBatches(ctx context.Context, page, perPage int) ([]Batch, int, error) {
	page, perPage = normalizePage(page, perPage)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM command_batches`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+batchCols+` FROM command_batches ORDER BY created_at DESC, id LIMIT $1 OFFSET $2`, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	out := []Batch{}
	var ids []uuid.UUID
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, b)
		ids = append(ids, b.ID)
	}
	rows.Close()
	counts, err := s.batchCounts(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Counts = withAllStatuses(counts[out[i].ID])
	}
	return out, total, nil
}

// PurgeCommandOutput vacía la salida de las órdenes terminadas antes de before.
func (s *Store) PurgeCommandOutput(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE commands SET stdout = '', stderr = '' WHERE finished_at < $1 AND (stdout <> '' OR stderr <> '')`, before)
	return tag.RowsAffected(), err
}

// emitCommandFailed publica command.failed con el script, si lo hay.
func emitCommandFailed(ctx context.Context, q Querier, cmdID, deviceID uuid.UUID, typ, msg string, at time.Time) error {
	data := map[string]any{"command_id": cmdID.String(), "type": typ, "error": CleanText(msg, 500)}
	var scriptID string
	if typ == protocol.CmdRunScript {
		_ = q.QueryRow(ctx, `SELECT COALESCE(payload->>'script_id', '') FROM commands WHERE id = $1`, cmdID).Scan(&scriptID)
		data["script_id"] = scriptID
	}
	return emit(ctx, q, Event{Type: EventCommandFailed, DeviceID: devicePtr(deviceID), At: at, Data: data})
}
