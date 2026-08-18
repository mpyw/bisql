// Package schema implements the schema.Migrator port: it applies the embedded schema-and-seed
// DDL (schema.sql) to a *sql.DB.
package schema

import (
	"context"
	"database/sql"
	_ "embed"

	port "github.com/mpyw/bisql/example/src/app/schema"
)

//go:embed schema.sql
var ddl string

// Migrator applies the schema to a database.
type Migrator struct {
	db *sql.DB
}

var _ port.Migrator = (*Migrator)(nil)

// New returns a Migrator backed by db.
func New(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Migrate creates the schema and seeds the database.
func (m *Migrator) Migrate(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, ddl)
	return err
}
