//go:build integration

// Package bisql_test integration checks: run bisql-generated SQL against a real Postgres to
// confirm it parses, type-checks, and (for array binds) actually works. Excluded from the
// default build; run with:
//
//	BISQL_TEST_PG_DSN='postgres://user:pw@localhost:5432/db?sslmode=disable' \
//	  go test -tags integration -run TestIntegrationPostgres ./...
package bisql_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

func TestIntegrationPostgres(t *testing.T) {
	dsn := os.Getenv("BISQL_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set BISQL_TEST_PG_DSN to run the Postgres integration test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	cases := []struct {
		name   string
		tmpl   string
		params map[string]any
	}{
		{"scalar cast", "select /*n*/1::bigint", map[string]any{"n": 42}},
		{"cast fn", "select cast(/*s*/'x' as text)", map[string]any{"s": "hi"}},
		{"in expand", "select 1 where 1 in /*ids*/(0)", map[string]any{"ids": []int{1, 2, 3}}},
		{"tuple in", "select 1 where (1, 2) in /*p*/((0, 0))", map[string]any{"p": [][]int{{1, 2}, {3, 4}}}},
		{"array bind = ANY", "select 1 where 'a' = ANY(/*xs*/'{}'::text[])", map[string]any{"xs": []string{"a", "b"}}},
		{"timestamptz array", "select 1 where now() = ANY(/*ts*/'{}'::timestamptz[])",
			map[string]any{"ts": []time.Time{time.Now()}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl, bisql.WithDialect(dialect.PostgreSQL))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			stmt, err := tmpl.Build(c.params)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			// Query rather than Exec so PG parses + plans + executes the generated SQL.
			rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
			if err != nil {
				t.Fatalf("exec %q args=%#v: %v", stmt.SQL, stmt.Args, err)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatalf("rows: %v", err)
			}
		})
	}
}
