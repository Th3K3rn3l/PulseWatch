// Package migrations contains the embedded SQL schema of the service.
package migrations

import "embed"

// FS holds the SQL migration files.
//
//go:embed *.sql
var FS embed.FS

// SchemaFile is the name of the initial schema inside FS.
const SchemaFile = "001_init.sql"
