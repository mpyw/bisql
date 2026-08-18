package bisql_test

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// Complex, realistic end-to-end scenarios. Each template lives as a readable .sql file under
// testdata/e2e/<template>/input.sql; its expected output is checked against per-case golden
// files (pure SQL, so they keep a .sql extension and stay highlightable):
//
//	testdata/e2e/<template>/<case>.output.sql    (placeholder form)
//	testdata/e2e/<template>/<case>.embedded.sql  (values-embedded form, when checked)
//
// Regenerate with: go test ./... -run TestE2E -update
var update = flag.Bool("update", false, "update golden files in testdata/")

func e2eDir(template string) string { return filepath.Join("testdata", "e2e", template) }

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

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

type goldenCase struct {
	name     string
	opts     []bisql.Option
	params   map[string]any
	args     []any
	embedded bool
}

func runGolden(t *testing.T, template string, cases []goldenCase) {
	t.Helper()
	dir := e2eDir(template)
	src := readFileString(t, filepath.Join(dir, "input.sql"))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(src, c.opts...)
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
			checkGolden(t, filepath.Join(dir, c.name+".output.sql"), stmt.SQL)
			if c.embedded {
				checkGolden(t, filepath.Join(dir, c.name+".embedded.sql"), stmt.SQLWithArgs())
			}
		})
	}
}

func pg() []bisql.Option { return []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)} }

// runGoldenInclude drives an @include-based scenario end to end. The template tree lives under
// testdata/e2e/<template>/ (input.sql + fragment files under _frag/). It checks both paths:
//
//   - expand:  bisql.ExpandFile resolves every @include into one still-two-way SQL golden
//     (expanded.sql), dialect-independent.
//   - render:  bisql.ParseFile + Build produces the final (SQL, Args) per case golden.
func runGoldenInclude(t *testing.T, template, root string, cases []goldenCase) {
	t.Helper()
	dir := e2eDir(template)
	fsys := os.DirFS(dir)

	expanded, err := bisql.ExpandFile(fsys, root)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	checkGolden(t, filepath.Join(dir, "expanded.sql"), expanded)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.ParseFile(fsys, root, c.opts...)
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
			checkGolden(t, filepath.Join(dir, c.name+".output.sql"), stmt.SQL)
			if c.embedded {
				checkGolden(t, filepath.Join(dir, c.name+".embedded.sql"), stmt.SQLWithArgs())
			}
		})
	}
}

// Recursive @include end to end: an audit-log report whose WHERE clause is assembled from
// reusable filter fragments (input -> _frag/status, and input -> _frag/window -> _frag/since).
// Checks both the expanded two-way text and the rendered (SQL, Args) across branch selections.
func TestE2EReport(t *testing.T) {
	runGoldenInclude(t, "report", "input.sql", []goldenCase{
		{name: "pg_all", opts: pg(), params: map[string]any{"activeOnly": true, "status": "active", "since": "2025-01-01", "until": "2025-12-31"}, args: []any{"active", "2025-01-01", "2025-12-31"}, embedded: true},
		{name: "pg_since_only", opts: pg(), params: map[string]any{"since": "2025-01-01"}, args: []any{"2025-01-01"}},
		{name: "mysql_none", params: map[string]any{}},
	})
}

func TestE2EDynamicWhere(t *testing.T) {
	runGolden(t, "dynamic_where", []goldenCase{
		{name: "all_set", params: map[string]any{"name": "Alice", "age": 30, "ids": []any{1, 2, 3}}, args: []any{"Alice", 30, 1, 2, 3}, embedded: true},
		{name: "age_only", params: map[string]any{"age": 30}, args: []any{30}, embedded: true},
		{name: "none", params: map[string]any{}},
	})
}

// Keyword search: 1=0 OR-anchor + a for-loop whose body is a self-contained "OR (...)" with
// two /* kw */ binds per keyword (name + email). Checked on MySQL and Postgres (locks global
// numbering) and with an empty keyword list.
func TestE2EKeywordSearch(t *testing.T) {
	kws := map[string]any{"keywords": []any{"%ali%", "%bob%"}}
	runGolden(t, "keyword_search", []goldenCase{
		{name: "mysql_two", params: kws, args: []any{"%ali%", "%ali%", "%bob%", "%bob%"}},
		{name: "postgres_two", opts: pg(), params: kws, args: []any{"%ali%", "%ali%", "%bob%", "%bob%"}},
		{name: "empty", params: map[string]any{"keywords": []any{}}},
	})
}

// The single integration fixture: one realistic query that exercises every directive kind at
// once — a CTE bind, an if/elseif/else branch on status, an IN-list bind, a tuple IN (row
// binds), a /*%for*/ keyword list with a separator, an order-by conditional, and a /*^ */ inline
// literal. Run across all four dialects on the full case to lock placeholder numbering, plus the
// pending and else branches with empty/absent inputs, with values-embedded snapshots.
func TestE2EAllInOne(t *testing.T) {
	full := map[string]any{"flag": true, "status": "active", "ids": []any{1, 2, 3}, "pairs": []any{[]any{1, "active"}}, "keywords": []any{"%ali%", "%bob%"}, "byName": true, "limit": 50}
	fullArgs := []any{true, 1, 2, 3, 1, "active", "%ali%", "%bob%"}
	runGolden(t, "all_in_one", []goldenCase{
		{name: "mysql_full", params: full, args: fullArgs, embedded: true},
		{name: "postgres_full", opts: pg(), params: full, args: fullArgs, embedded: true},
		{name: "oracle_full", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: full, args: fullArgs},
		{name: "sqlserver_full", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: full, args: fullArgs},
		{
			name:     "mysql_pending",
			params:   map[string]any{"flag": false, "status": "pending", "pairs": []any{[]any{3, "banned"}}, "keywords": []any{}, "byName": false, "limit": 10},
			args:     []any{false, 3, "banned"},
			embedded: true,
		},
		{
			name:   "pg_else",
			opts:   pg(),
			params: map[string]any{"flag": false, "status": "banned", "pairs": []any{[]any{9, "active"}}, "keywords": []any{"%x%"}, "byName": false, "limit": 5},
			args:   []any{false, 9, "active", "%x%"},
		},
	})
}
