// Package migrations incrusta las migraciones SQL en el binario del servidor.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
