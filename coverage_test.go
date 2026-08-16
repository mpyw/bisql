package bisql_test

import (
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/pkg/dialect"
)

// buildCase is a full-pipeline expectation: parse tmpl, build with params, compare SQL,
// Args, and (when withArgs is set) the values-embedded form.
type buildCase struct {
	name     string
	tmpl     string
	params   map[string]any
	sql      string
	args     []any
	withArgs string // optional; checked only when non-empty
}

func runBuild(t *testing.T, opts []bisql.Option, cases []buildCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl, opts...)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			stmt, err := tmpl.Build(c.params)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if stmt.SQL != c.sql {
				t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, c.sql)
			}
			if !reflect.DeepEqual(nilIfEmpty(stmt.Args), nilIfEmpty(c.args)) {
				t.Errorf("Args\n got: %#v\nwant: %#v", stmt.Args, c.args)
			}
			if c.withArgs != "" && stmt.SQLWithArgs != c.withArgs {
				t.Errorf("SQLWithArgs\n got: %q\nwant: %q", stmt.SQLWithArgs, c.withArgs)
			}
		})
	}
}

func nilIfEmpty(a []any) []any {
	if len(a) == 0 {
		return nil
	}
	return a
}

// --- §6 if / elseif / else chain ---

func TestIfElseifElse(t *testing.T) {
	const tmpl = "select 1 from t where /*%if a == 1*/x = 1/*%elseif a == 2*/y = 2/*%else*/z = 3/*%end*/"
	runBuild(t, nil, []buildCase{
		{name: "if branch", tmpl: tmpl, params: map[string]any{"a": 1}, sql: "select 1 from t where x = 1"},
		{name: "elseif branch", tmpl: tmpl, params: map[string]any{"a": 2}, sql: "select 1 from t where y = 2"},
		{name: "else branch", tmpl: tmpl, params: map[string]any{"a": 9}, sql: "select 1 from t where z = 3"},
	})
}

// --- §7 for: all helper variables ---

func TestForHelpers(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "index and has_next",
			tmpl:   "select /*%for i in xs*//*# i_index *//*%if i_has_next*/,/*%end*//*%end*/ from t",
			params: map[string]any{"xs": []any{"a", "b", "c"}},
			sql:    "select 0,1,2 from t",
		},
		{
			name:   "next_and joins conditions",
			tmpl:   "select 1 from t where /*%for i in xs*/c = /*i*/0 /*# i_next_and */ /*%end*/",
			params: map[string]any{"xs": []any{10, 20}},
			sql:    "select 1 from t where c = ? and c = ?  ",
			args:   []any{10, 20},
		},
		{
			name:   "empty list renders nothing (WHERE removed, FROM trailing space kept)",
			tmpl:   "select 1 from t where /*%for i in xs*/c = /*i*/0/*%end*/",
			params: map[string]any{"xs": []any{}},
			sql:    "select 1 from t ",
		},
	})
}

// --- §8 with block: exposes a value's members as scope variables ---

func TestWithBlock(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "map members",
			tmpl:   "select 1 from t where /*%with user*/name = /*name*/'x' and age > /*age*/0/*%end*/",
			params: map[string]any{"user": map[string]any{"name": "bob", "age": 20}},
			sql:    "select 1 from t where name = ? and age > ?",
			args:   []any{"bob", 20},
		},
	})
}

func TestWithBlockStruct(t *testing.T) {
	type user struct {
		Name string
		Age  int
	}
	runBuild(t, nil, []buildCase{
		{
			name:   "struct members",
			tmpl:   "select 1 from t where /*%with u*/name = /*Name*/'x'/*%end*/",
			params: map[string]any{"u": user{Name: "carol", Age: 30}},
			sql:    "select 1 from t where name = ?",
			args:   []any{"carol"},
		},
	})
}

// --- whitespace normalization across newlines (keep-from-last-EOL) ---

func TestMultilineWhitespace(t *testing.T) {
	const tmpl = "select 1\nfrom t\nwhere\n  /*%if show*/a = 1/*%end*/\norder by a"
	runBuild(t, nil, []buildCase{
		{
			name:   "kept",
			tmpl:   tmpl,
			params: map[string]any{"show": true},
			sql:    "select 1\nfrom t\nwhere\n  a = 1\norder by a",
		},
		{
			name:   "dropped where collapses blank lines",
			tmpl:   tmpl,
			params: map[string]any{"show": false},
			sql:    "select 1\nfrom t\n\norder by a",
		},
	})
}

// --- dialect placeholders ---

func TestDialectPlaceholders(t *testing.T) {
	const tmpl = "select 1 from t where a = /*a*/0 and b in /*bs*/(1)"
	params := map[string]any{"a": 1, "bs": []any{2, 3}}
	cases := []struct {
		name string
		d    dialect.Dialect
		sql  string
	}{
		{"mysql", dialect.MySQL, "select 1 from t where a = ? and b in (?, ?)"},
		{"postgres", dialect.PostgreSQL, "select 1 from t where a = $1 and b in ($2, $3)"},
		{"oracle", dialect.Oracle, "select 1 from t where a = :1 and b in (:2, :3)"},
		{"sqlserver", dialect.SQLServer, "select 1 from t where a = @p1 and b in (@p2, @p3)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(tmpl, bisql.WithDialect(c.d))
			if err != nil {
				t.Fatal(err)
			}
			stmt, err := tmpl.Build(params)
			if err != nil {
				t.Fatal(err)
			}
			if stmt.SQL != c.sql {
				t.Errorf("got %q want %q", stmt.SQL, c.sql)
			}
		})
	}
}

// --- SQLWithArgs: literal formatting of various types ---

func TestSQLWithArgsTypes(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "nil bool string int",
			tmpl:     "values (/*a*/0, /*b*/0, /*c*/0, /*d*/0)",
			params:   map[string]any{"a": nil, "b": true, "c": "it's", "d": 42},
			sql:      "values (?, ?, ?, ?)",
			args:     []any{nil, true, "it's", 42},
			withArgs: "values (null, true, 'it''s', 42)",
		},
		{
			name:     "in list embedded",
			tmpl:     "select 1 from t where id in /*ids*/(0)",
			params:   map[string]any{"ids": []any{1, 2}},
			sql:      "select 1 from t where id in (?, ?)",
			args:     []any{1, 2},
			withArgs: "select 1 from t where id in (1, 2)",
		},
	})
}

// --- multi-column (tuple) IN expansion ---

func TestTupleIN(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "pairs",
			tmpl:     "select 1 from t where (a, b) in /*pairs*/((0, 0))",
			params:   map[string]any{"pairs": []any{[]any{1, 2}, []any{3, 4}}},
			sql:      "select 1 from t where (a, b) in ((?, ?), (?, ?))",
			args:     []any{1, 2, 3, 4},
			withArgs: "select 1 from t where (a, b) in ((1, 2), (3, 4))",
		},
	})
}

// --- struct params (top-level) ---

func TestStructParams(t *testing.T) {
	type params struct {
		Name string
		Age  int
	}
	tmpl, err := bisql.Parse("select 1 from t where name = /*Name*/'x' and age > /*Age*/0")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(params{Name: "dave", Age: 40})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "select 1 from t where name = ? and age > ?" {
		t.Errorf("SQL got %q", stmt.SQL)
	}
	if !reflect.DeepEqual(stmt.Args, []any{"dave", 40}) {
		t.Errorf("Args got %#v", stmt.Args)
	}
	// pointer to struct also works
	if _, err := tmpl.Build(&params{Name: "x", Age: 1}); err != nil {
		t.Errorf("pointer struct: %v", err)
	}
}

// --- §2 literal value directive: type variants ---

func TestLiteralVariants(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "null",
			tmpl:   "select 1 where a = /*^v*/'x'",
			params: map[string]any{"v": nil},
			sql:    "select 1 where a = null",
		},
		{
			name:   "number",
			tmpl:   "select 1 where a = /*^v*/'x'",
			params: map[string]any{"v": 42},
			sql:    "select 1 where a = 42",
		},
		{
			name:   "string escaped",
			tmpl:   "select 1 where a = /*^v*/'x'",
			params: map[string]any{"v": "o'brien"},
			sql:    "select 1 where a = 'o''brien'",
		},
	})
}

func TestLiteralUnformattable(t *testing.T) {
	tmpl, err := bisql.Parse("select 1 where a = /*^v*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	// A literal directive formats via the dialect; an unformattable value is an error
	// (unlike SQLWithArgs, which is best-effort).
	if _, err := tmpl.Build(map[string]any{"v": []byte{1, 2}}); err == nil {
		t.Fatal("expected literal-format error")
	}
}

// --- §8 with block error paths ---

func TestWithBlockErrors(t *testing.T) {
	nilCase, err := bisql.Parse("select 1 /*%with u*/x/*%end*/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nilCase.Build(map[string]any{"u": nil}); err == nil {
		t.Error("expected error for nil with-receiver")
	}
	if _, err := nilCase.Build(map[string]any{"u": 5}); err == nil {
		t.Error("expected error for non-struct/map with-receiver")
	}
}

// A malformed embedded string is reported as a parse error at Build time.
func TestEmbeddedParseError(t *testing.T) {
	tmpl, err := bisql.Parse("select /*# frag */")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Build(map[string]any{"frag": "select ("}); err == nil {
		t.Fatal("expected embedded parse error")
	}
}

// --- comments pass through; parser comments are removed ---
//
// Note the 2-way rule: /* expr */ is a bind directive, so a plain block comment must start
// with a non-identifier char (here /** ... */) to avoid being read as a bind.
func TestComments(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "block comment kept",
			tmpl:   "select 1 /** keep me */ from t",
			params: nil,
			sql:    "select 1 /** keep me */ from t",
		},
		{
			name:   "parser comment removed",
			tmpl:   "select 1 /*%! drop me */ from t",
			params: nil,
			sql:    "select 1  from t",
		},
	})
}
