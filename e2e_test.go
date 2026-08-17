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
// plus a values-embedded snapshot.
func TestE2EAllInOne(t *testing.T) {
	full := map[string]any{"flag": true, "name": "SCOTT", "ids": []any{1, 2}, "byName": true}
	dia := map[string]any{"flag": true, "name": "SCOTT", "ids": []any{1, 2}, "byName": true}
	args := []any{true, "SCOTT", 1, 2}
	runGolden(t, "all_in_one", []goldenCase{
		{name: "mysql_full", params: full, args: args, embedded: true},
		{name: "mysql_min", params: map[string]any{"flag": false}, args: []any{false}, embedded: true},
		{name: "postgres", opts: pg(), params: dia, args: args},
		{name: "oracle", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: dia, args: args},
		{name: "sqlserver", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: dia, args: args},
	})
}
