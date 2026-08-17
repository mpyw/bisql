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

// @include end to end (recursive: input -> _frag/active -> _frag/dept), checking both the
// expanded two-way text and the rendered (SQL, Args) across dialects and branch selections.
func TestE2EInclude(t *testing.T) {
	runGoldenInclude(t, "include_search", "input.sql", []goldenCase{
		{name: "pg_all", opts: pg(), params: map[string]any{"activeOnly": true, "zero": 0, "dept": 10, "name": "SCOTT"}, args: []any{0, 10, "SCOTT"}, embedded: true},
		{name: "pg_name_only", opts: pg(), params: map[string]any{"name": "SCOTT"}, args: []any{"SCOTT"}},
		{name: "mysql_none", params: map[string]any{}},
	})
}

func TestE2EDynamicWhere(t *testing.T) {
	runGolden(t, "dynamic_where", []goldenCase{
		{name: "all_set", params: map[string]any{"name": "SCOTT", "age": 20, "ids": []any{1, 2, 3}}, args: []any{"SCOTT", 20, 1, 2, 3}, embedded: true},
		{name: "age_only", params: map[string]any{"age": 20}, args: []any{20}, embedded: true},
		{name: "none", params: map[string]any{}},
	})
}

// Keyword search: 1=0 OR-anchor + a for-loop whose body is a self-contained "OR (...)" with
// three /* kw */ binds per keyword. Checked on MySQL and Postgres (locks global numbering)
// and with an empty keyword list.
func TestE2EKeywordSearch(t *testing.T) {
	kws := map[string]any{"keywords": []any{"%a%", "%b%"}}
	runGolden(t, "keyword_search", []goldenCase{
		{name: "mysql_two", params: kws, args: []any{"%a%", "%a%", "%a%", "%b%", "%b%", "%b%"}},
		{name: "postgres_two", opts: pg(), params: kws, args: []any{"%a%", "%a%", "%a%", "%b%", "%b%", "%b%"}},
		{name: "empty", params: map[string]any{"keywords": []any{}}},
	})
}

// All-in-one across all four dialects (locks placeholder numbering across CTE + WHERE + IN),
// with a values-embedded snapshot on one MySQL and one Postgres case.
func TestE2EAllInOne(t *testing.T) {
	full := map[string]any{"flag": true, "name": "SCOTT", "ids": []any{1, 2}, "byName": true}
	args := []any{true, "SCOTT", 1, 2}
	runGolden(t, "all_in_one", []goldenCase{
		{name: "mysql_full", params: full, args: args, embedded: true},
		{name: "mysql_min", params: map[string]any{"flag": false}, args: []any{false}, embedded: true},
		{name: "postgres", opts: pg(), params: full, args: args, embedded: true},
		{name: "oracle", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: full, args: args},
		{name: "sqlserver", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: full, args: args},
	})
}

// Mixed directives in one realistic query: an if/elseif/else branch, a tuple IN (row binds), a
// /*%for*/ keyword list with a separator, and a /*^ */ inline literal. Checked on Postgres
// (gold + else branches) and MySQL (silver branch, empty keyword list), with embedded snapshots.
func TestE2EMixed(t *testing.T) {
	runGolden(t, "mixed", []goldenCase{
		{
			name:     "pg_gold",
			opts:     pg(),
			params:   map[string]any{"tier": "gold", "pairs": []any{[]any{1, "x"}, []any{2, "y"}}, "keywords": []any{"%a%", "%b%"}, "limit": 50},
			args:     []any{1, "x", 2, "y", "%a%", "%b%"},
			embedded: true,
		},
		{
			name:     "mysql_silver",
			params:   map[string]any{"tier": "silver", "pairs": []any{[]any{3, "z"}}, "keywords": []any{}, "limit": 10},
			args:     []any{3, "z"},
			embedded: true,
		},
		{
			name:   "pg_bronze_else",
			opts:   pg(),
			params: map[string]any{"tier": "bronze", "pairs": []any{[]any{9, "q"}}, "keywords": []any{"%x%"}, "limit": 5},
			args:   []any{9, "q", "%x%"},
		},
	})
}
