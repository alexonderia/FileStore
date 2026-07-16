package migrations

import "embed"

// Files contains immutable database migrations.
//
//go:embed *.up.sql
var Files embed.FS
