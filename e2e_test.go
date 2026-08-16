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

// Complex, realistic end-to-end scenarios. The templates with non-trivial control
// structure live as readable .sql files under testdata/, and their expected output is
// checked against golden files there. Regenerate the goldens with:
//
//	go test ./... -run TestE2E -update
var update = flag.Bool("update", false, "update golden files in testdata/")

// e2eDir is the directory holding one template's input and golden outputs:
//
//	testdata/e2e/<template>/input.sql
//	testdata/e2e/<template>/<case>.output.sql        (placeholder form)
//	testdata/e2e/<template>/<case>.embedded.sql      (values-embedded form, when checked)
func e2eDir(template string) string { return filepath.Join("testdata", "e2e", template) }

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// checkGolden compares (or, with -update, writes) one golden file. Golden files are pure
// SQL so they keep a .sql extension and stay syntax-highlightable.
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

// goldenCase drives one template across named parameter sets. Each case goldens the
// placeholder-form SQL to <case>.output.sql, asserts its bind args inline (short, and not
// SQL), and — when embedded is set — goldens the values-embedded form (the runnable 2-way
// SQL) to <case>.embedded.sql.
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
				checkGolden(t, filepath.Join(dir, c.name+".embedded.sql"), stmt.SQLWithArgs)
			}
		})
	}
}

func TestE2EDynamicWhere(t *testing.T) {
	runGolden(t, "dynamic_where", []goldenCase{
		{name: "all_set", params: map[string]any{"name": "SCOTT", "age": 20, "ids": []any{1, 2, 3}}, args: []any{"SCOTT", 20, 1, 2, 3}, embedded: true},
		{name: "none_set", params: map[string]any{}},
	})
	runGolden(t, "dynamic_where_no_idiom", []goldenCase{
		{name: "age_only", params: map[string]any{"age": 20}, args: []any{20}, embedded: true},
		{name: "none_set", params: map[string]any{}},
	})
}

func TestE2EDynamicOrderBy(t *testing.T) {
	runGolden(t, "dynamic_order_by", []goldenCase{
		{name: "with_sorts", params: map[string]any{"sorts": []any{"name asc", "age desc"}}},
		{name: "no_sorts", params: map[string]any{}},
	})
}

func TestE2EForAndJoin(t *testing.T) {
	runGolden(t, "for_and_join", []goldenCase{
		{name: "three", params: map[string]any{"conds": []any{1, 2, 3}}, args: []any{1, 2, 3}, embedded: true},
	})
}

// The all-in-one across dialects also locks placeholder numbering across the CTE, WHERE,
// and IN (the shape the numbering bug corrupted).
func TestE2EAllInOne(t *testing.T) {
	full := map[string]any{"flag": true, "cols": []any{"p.id", "p.name"}, "name": "SCOTT", "ids": []any{1, 2}, "sorts": []any{"name", "id desc"}}
	dia := map[string]any{"flag": true, "cols": []any{"p.id"}, "name": "SCOTT", "ids": []any{1, 2}}
	fullArgs := []any{true, "SCOTT", 1, 2}
	runGolden(t, "all_in_one", []goldenCase{
		{name: "mysql_full", params: full, args: fullArgs, embedded: true},
		{name: "mysql_min", params: map[string]any{"flag": false, "cols": []any{"p.id"}}, args: []any{false}, embedded: true},
		{name: "postgres", opts: []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}, params: dia, args: fullArgs},
		{name: "oracle", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: dia, args: fullArgs},
		{name: "sqlserver", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: dia, args: fullArgs},
	})
}

// --- shorter cases kept inline (no complex control structure) ---

func TestE2ECTE(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "simple cte",
			tmpl:     "with cte as (select id from users where active = /*active*/true) select id from cte order by id",
			params:   map[string]any{"active": true},
			sql:      "with cte as (select id from users where active = ?) select id from cte order by id",
			args:     []any{true},
			withArgs: "with cte as (select id from users where active = true) select id from cte order by id",
		},
		{
			name:   "recursive cte",
			tmpl:   "with recursive t(n) as (select 1 union all select n+1 from t where n < /*max*/10) select n from t",
			params: map[string]any{"max": 10},
			sql:    "with recursive t(n) as (select 1 union all select n+1 from t where n < ?) select n from t",
			args:   []any{10},
		},
		{
			name:   "cte with dynamic inner where removed",
			tmpl:   "with cte as (select id, name from users where /*%if name != null*/name = /*name*/'x'/*%end*/) select * from cte",
			params: map[string]any{"name": nil},
			sql:    "with cte as (select id, name from users ) select * from cte",
		},
	})
}

func TestE2ERecursiveIncludes(t *testing.T) {
	t.Run("nested partials a->b->c", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("a", "x = 1 /*> b */")
		ld.Register("b", "and y = 2 /*> c */")
		ld.Register("c", "and z = 3")
		tmpl, err := ld.Parse("select * from t where /*> a */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(nil)
		if stmt.SQL != "select * from t where x = 1 and y = 2 and z = 3" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("partial containing an embed", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("frag", "col = /*# raw */")
		tmpl, err := ld.Parse("select /*> frag */ from t")
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"raw": "computed"})
		if stmt.SQL != "select col = computed from t" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("embed producing a partial reference", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("cond", "active = /*a*/true")
		tmpl, err := ld.Parse("select * from t where /*# dyn */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"dyn": "/*> cond */", "a": true})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select * from t where active = ?" || !reflect.DeepEqual(stmt.Args, []any{true}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})

	t.Run("embed producing a bind", func(t *testing.T) {
		tmpl, err := bisql.Parse("select * from t where /*# dyn */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"dyn": "a = /*x*/1", "x": 99})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select * from t where a = ?" || !reflect.DeepEqual(stmt.Args, []any{99}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})
}

// Known authoring gotcha, pinned: an empty grouping paren is not removed (it is
// indistinguishable from a call like count()); guard a dynamic group with an outer if.
func TestE2EEmptyGroupingParens(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "kept",
			tmpl:   "select * from t where (/*%if a*/a = 1/*%end*/)",
			params: map[string]any{"a": false},
			sql:    "select * from t where ()",
		},
	})
}
