// Package migrations встраивает SQL-миграции goose в бинарник.
package migrations

import "embed"

// FS — все миграции *.sql из этой папки.
//
//go:embed *.sql
var FS embed.FS
