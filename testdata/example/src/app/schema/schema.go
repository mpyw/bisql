// Package schema defines the Migrator port: the application depends on the ability to bring a
// database up to the expected schema, without knowing how. The implementation lives in
// app/infrastructure/schema.
package schema

import "context"

// Migrator brings a database up to the schema the application expects (creating tables, seeding).
type Migrator interface {
	Migrate(ctx context.Context) error
}
