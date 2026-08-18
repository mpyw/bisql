package bisql_test

import (
	"flag"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// TestE2E exercises one comprehensive template that uses every directive kind at once — a CTE
// bind, a recursive @include of reusable fragments, an if/elseif/else branch, an IN-list bind, a
// PostgreSQL array bind, a tuple (row) IN, a /*%for*/ loop, an order-by conditional, and a /*^ */
// inline literal — rendered across all four dialects to lock placeholder numbering.
//
// All fixtures are flat files under testdata/e2e, named by the template and (per case) the case:
//
//	all_in_one.sql                  the template (@include-ing all_in_one.scope.sql, which in turn
//	                                @include-s all_in_one.active.sql)
//	all_in_one.expanded.sql         every @include resolved (dialect-independent)
//	all_in_one.<case>.prepared.sql  parameterized SQL (placeholder form)
//	all_in_one.<case>.embedded.sql  values-embedded SQL (when checked)
//
// Regenerate with: go test -run TestE2E -update
var update = flag.Bool("update", false, "update golden files in testdata/")

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with: go test -run TestE2E -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestE2E(t *testing.T) {
	const dir = "testdata/e2e"
	fsys := os.DirFS(dir)

	// Expanded snapshot: every @include resolved, still two-way, independent of dialect.
	expanded, err := bisql.ExpandFile(fsys, "all_in_one.sql")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	checkGolden(t, filepath.Join(dir, "all_in_one.expanded.sql"), expanded)

	// The full case supplies every optional predicate; rendered under all four dialects it locks
	// the placeholder numbering. pg_array adds the PostgreSQL-only array bind; the pending/else
	// cases select the other if/elseif/else branches with mostly-empty inputs.
	full := map[string]any{
		"flag": true, "activeOnly": true, "status": "active", "departmentId": 3,
		"ageBand": "adult", "ids": []any{1, 2, 3}, "pairs": []any{[]any{1, "active"}},
		"keywords": []any{"%a%", "%b%"}, "byName": true, "limit": 50,
	}
	fullArgs := []any{true, "active", 3, 1, 2, 3, 1, "active", "%a%", "%b%"}

	pgArray := maps.Clone(full)
	pgArray["tags"] = []string{"vip", "beta"}
	pgArrayArgs := []any{true, "active", 3, 1, 2, 3, []string{"vip", "beta"}, 1, "active", "%a%", "%b%"}

	cases := []struct {
		name     string
		opts     []bisql.Option
		params   map[string]any
		args     []any
		embedded bool
	}{
		{name: "mysql_full", params: full, args: fullArgs, embedded: true},
		{name: "postgres_full", opts: []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}, params: full, args: fullArgs, embedded: true},
		{name: "oracle_full", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: full, args: fullArgs},
		{name: "sqlserver_full", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: full, args: fullArgs},
		{name: "pg_array", opts: []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}, params: pgArray, args: pgArrayArgs, embedded: true},
		{
			name:     "mysql_pending",
			params:   map[string]any{"flag": false, "ageBand": "senior", "pairs": []any{[]any{3, "banned"}}, "keywords": []any{}, "byName": false, "limit": 10},
			args:     []any{false, 3, "banned"},
			embedded: true,
		},
		{
			name:   "pg_else",
			opts:   []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)},
			params: map[string]any{"flag": false, "pairs": []any{[]any{7, "active"}}, "keywords": []any{"%x%"}, "byName": false, "limit": 5},
			args:   []any{false, 7, "active", "%x%"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.ParseFile(fsys, "all_in_one.sql", c.opts...)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			stmt, err := tmpl.Build(c.params)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if !reflect.DeepEqual(stmt.Args, c.args) {
				t.Errorf("Args\n got: %#v\nwant: %#v", stmt.Args, c.args)
			}
			checkGolden(t, filepath.Join(dir, "all_in_one."+c.name+".prepared.sql"), stmt.SQL)
			if c.embedded {
				checkGolden(t, filepath.Join(dir, "all_in_one."+c.name+".embedded.sql"), stmt.SQLWithArgs())
			}
		})
	}
}
