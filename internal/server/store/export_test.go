package store

import (
	"context"

	"github.com/google/uuid"
)

// DeleteAdminForTest borra un administrador. La aplicación nunca los borra
// (solo los desactiva), pero las pruebas necesitan un identificador inexistente.
func (s *Store) DeleteAdminForTest(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM admins WHERE id = $1`, id)
	return err
}

// PurgeAssetForTest borra un activo sin pasar por la papelera.
func (s *Store) PurgeAssetForTest(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assets WHERE id = $1`, id)
	return err
}
