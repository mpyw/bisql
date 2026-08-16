package bisql_test

import (
	"flag"
	"fmt"
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

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// formatStmt renders a Statement into the stable, human-readable form stored in goldens.
func formatStmt(s bisql.Statement) string {
	return fmt.Sprintf("SQL:\n%s\n\nArgs: %#v\n\nSQLWithArgs:\n%s\n", s.SQL, s.Args, s.SQLWithArgs)
}

// checkGolden compares (or, with -update, writes) the golden for one case.
func checkGolden(t *testing.T, golden string, stmt bisql.Statement) {
	t.Helper()
	got := formatStmt(stmt)
	path := filepath.Join("testdata", golden)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with: go test -run TestE2E -update)", golden, err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

// goldenCase drives a template (from testdata) across named parameter sets, each checked
// against testdata/<base>.<case>.golden.
type goldenCase struct {
	name   string
	opts   []bisql.Option
	params map[string]any
}

func runGolden(t *testing.T, tmplFile string, cases []goldenCase) {
	t.Helper()
	src := readTestdata(t, tmplFile)
	base := tmplFile[:len(tmplFile)-len(filepath.Ext(tmplFile))]
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
			checkGolden(t, base+"."+c.name+".golden", stmt)
		})
	}
}

func TestE2EDynamicWhere(t *testing.T) {
	runGolden(t, "e2e_dynamic_where.sql", []goldenCase{
		{name: "all_set", params: map[string]any{"name": "SCOTT", "age": 20, "ids": []any{1, 2, 3}}},
		{name: "none_set", params: map[string]any{}},
	})
	runGolden(t, "e2e_dynamic_where_no_idiom.sql", []goldenCase{
		{name: "age_only", params: map[string]any{"age": 20}},
		{name: "none_set", params: map[string]any{}},
	})
}

func TestE2EDynamicOrderBy(t *testing.T) {
	runGolden(t, "e2e_dynamic_order_by.sql", []goldenCase{
		{name: "with_sorts", params: map[string]any{"sorts": []any{"name asc", "age desc"}}},
		{name: "no_sorts", params: map[string]any{}},
	})
}

func TestE2EForAndJoin(t *testing.T) {
	runGolden(t, "e2e_for_and_join.sql", []goldenCase{
		{name: "three", params: map[string]any{"conds": []any{1, 2, 3}}},
	})
}

// The all-in-one across dialects also locks placeholder numbering across the CTE, WHERE,
// and IN (the shape the numbering bug corrupted).
func TestE2EAllInOne(t *testing.T) {
	full := map[string]any{"flag": true, "cols": []any{"p.id", "p.name"}, "name": "SCOTT", "ids": []any{1, 2}, "sorts": []any{"name", "id desc"}}
	dia := map[string]any{"flag": true, "cols": []any{"p.id"}, "name": "SCOTT", "ids": []any{1, 2}}
	runGolden(t, "e2e_all_in_one.sql", []goldenCase{
		{name: "mysql_full", params: full},
		{name: "mysql_min", params: map[string]any{"flag": false, "cols": []any{"p.id"}}},
		{name: "postgres", opts: []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}, params: dia},
		{name: "oracle", opts: []bisql.Option{bisql.WithDialect(dialect.Oracle)}, params: dia},
		{name: "sqlserver", opts: []bisql.Option{bisql.WithDialect(dialect.SQLServer)}, params: dia},
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
