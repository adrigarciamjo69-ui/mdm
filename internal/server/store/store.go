// Package store es la única pieza del servidor que conoce SQL.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // controlador database/sql para goose
	"github.com/pressly/goose/v3"

	"mdm/migrations"
)

var (
	// ErrNotFound indica que la fila pedida no existe.
	ErrNotFound = errors.New("no encontrado")
	// ErrConflict indica que la operación choca con una restricción (duplicado o regla de negocio).
	ErrConflict = errors.New("conflicto")
)

type Store struct {
	pool *pgxpool.Pool
}

// Open abre el grupo de conexiones y comprueba que la base de datos responde.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("abrir base de datos: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("conectar con la base de datos: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Migrate aplica las migraciones pendientes. Es seguro llamarla en cada arranque.
func Migrate(ctx context.Context, url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("abrir base de datos para migrar: %w", err)
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("preparar migraciones: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("aplicar migraciones: %w", err)
	}
	return nil
}

// TableExists informa de si la tabla existe en el esquema actual.
func (s *Store) TableExists(ctx context.Context, name string) bool {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		                WHERE table_schema = current_schema() AND table_name = $1)`, name).Scan(&ok)
	return err == nil && ok
}

// isUniqueViolation reconoce el error de PostgreSQL por clave duplicada.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// isForeignKeyViolation reconoce una referencia a una fila inexistente.
func isForeignKeyViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}
