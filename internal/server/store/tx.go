package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier lo satisfacen el grupo de conexiones y una transacción: permite
// escribir funciones que funcionan dentro o fuera de una transacción.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Pool devuelve el grupo de conexiones como Querier.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Tx ejecuta fn en una transacción; si fn devuelve error, se deshace.
func (s *Store) Tx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
