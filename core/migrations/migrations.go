// Package migrations embeds the SQLite schema migrations of the core.
//
// Files are named NNNN_description.sql; NNNN is the schema version stored in
// PRAGMA user_version after the file is applied. See docs/05-data-model.md.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var files embed.FS

// FS returns the read-only set of migration files.
func FS() fs.FS { return files }
