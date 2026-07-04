package migrations

import "embed"

// FS embeds all forward-only PostgreSQL migrations into release binaries.
//
//go:embed *.sql
var FS embed.FS
