package bisql_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
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

// A cast (or any ':'-led suffix) after a bind's test literal is preserved: ':' is not a
// word character, so it is not absorbed into the test literal and replaced with the value.
func TestBindCastSuffix(t *testing.T) {
	pg := []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}
	cases := []struct {
		name   string
		tmpl   string
		params map[string]any
		sql    string
		args   []any
	}{
		{"number cast", "select * from t where n = /*n*/1::bigint", map[string]any{"n": 1}, "select * from t where n = $1::bigint", []any{1}},
		{"float cast", "select * from t where n = /*n*/1.5::numeric", map[string]any{"n": 1.5}, "select * from t where n = $1::numeric", []any{1.5}},
		{"quote cast", "select * from t where c = /*ts*/'x'::timestamptz", map[string]any{"ts": "2020-01-01"}, "select * from t where c = $1::timestamptz", []any{"2020-01-01"}},
		{"cast then and", "select * from t where a = /*a*/1::int and b = /*b*/2", map[string]any{"a": 1, "b": 2}, "select * from t where a = $1::int and b = $2", []any{1, 2}},
		// cast on the column, then an expanded IN list (a realistic combination).
		{"column cast with in", "select * from t where x::text in /*xs*/('a')", map[string]any{"xs": []any{"a", "b"}}, "select * from t where x::text in ($1, $2)", []any{"a", "b"}},
		// CAST(... AS ...) function form: a bind inside parens with a trailing " AS type".
		{"cast fn quote", "select * from t where c = CAST(/*ts*/'x' AS timestamptz)", map[string]any{"ts": "2020-01-01"}, "select * from t where c = CAST($1 AS timestamptz)", []any{"2020-01-01"}},
		{"cast fn number", "select * from t where n = CAST(/*n*/1 AS bigint)", map[string]any{"n": 1}, "select * from t where n = CAST($1 AS bigint)", []any{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl, pg...)
			if err != nil {
				t.Fatal(err)
			}
			stmt, err := tmpl.Build(c.params)
			if err != nil {
				t.Fatal(err)
			}
			if stmt.SQL != c.sql {
				t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, c.sql)
			}
			if !reflect.DeepEqual(stmt.Args, c.args) {
				t.Errorf("Args got %#v want %#v", stmt.Args, c.args)
			}
		})
	}
}

// HAVING behaves like WHERE: dynamic conditions drop a dangling leading AND, and an empty
// HAVING is removed.
func TestDynamicHaving(t *testing.T) {
	const tmpl = "select x, count(*) from t group by x having /*%if a*/and count(*) > /*n*/1/*%end*/ /*%if b*/and sum(y) > /*m*/2/*%end*/"
	runBuild(t, nil, []buildCase{
		{
			name:   "second only drops leading and",
			tmpl:   tmpl,
			params: map[string]any{"b": true, "m": 2},
			sql:    "select x, count(*) from t group by x having   sum(y) > ?",
			args:   []any{2},
		},
		{
			name:   "none removes having",
			tmpl:   tmpl,
			params: map[string]any{},
			sql:    "select x, count(*) from t group by x ",
		},
	})
}

// A subquery's own WHERE is dropped/trimmed independently of the outer query (the paren
// contents render in their own availability scope).
func TestSubqueryDynamicWhere(t *testing.T) {
	const tmpl = "select * from (select id from t where /*%if a*/and x = /*x*/1/*%end*/) s where /*%if b*/and y = /*y*/2/*%end*/"
	runBuild(t, nil, []buildCase{
		{
			name:   "inner only",
			tmpl:   tmpl,
			params: map[string]any{"a": true, "x": 1},
			sql:    "select * from (select id from t where  x = ?) s ",
			args:   []any{1},
		},
		{
			name:   "outer only",
			tmpl:   tmpl,
			params: map[string]any{"b": true, "y": 2},
			sql:    "select * from (select id from t ) s where  y = ?",
			args:   []any{2},
		},
		{
			name:   "none",
			tmpl:   tmpl,
			params: map[string]any{},
			sql:    "select * from (select id from t ) s ",
		},
	})
}

// A bare /*%if flag*/ with flag nil/absent is falsy (no defensive guard needed); a non-nil,
// non-boolean condition is still an error (see TestBuildErrors).
func TestIfNilIsFalsy(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "absent flag is false",
			tmpl:   "select 1 from t /*%if activeOnly*/where retired = 0/*%end*/",
			params: map[string]any{},
			sql:    "select 1 from t ",
		},
		{
			name:   "true flag renders",
			tmpl:   "select 1 from t /*%if activeOnly*/where retired = 0/*%end*/",
			params: map[string]any{"activeOnly": true},
			sql:    "select 1 from t where retired = 0",
		},
	})
}

// for-loop variables and helpers restore pre-existing scope values afterwards (a caller key
// named like a helper is not clobbered).
func TestForRestoresShadowedScope(t *testing.T) {
	tmpl, err := bisql.Parse("/*%for i in xs*/x/*%end*/ tail=/*# i_index */")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"xs": []any{1, 2}, "i_index": "USER"})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "xx tail=USER" {
		t.Errorf("got %q, want %q (pre-existing i_index restored)", stmt.SQL, "xx tail=USER")
	}
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
		{
			// Arbitrary arity (here 4 columns), unlike Komapper's Pair/Triple-only support.
			name:     "quadruples",
			tmpl:     "select 1 from t where (a, b, c, d) in /*rows*/((0, 0, 0, 0))",
			params:   map[string]any{"rows": []any{[]any{1, 2, 3, 4}, []any{5, 6, 7, 8}}},
			sql:      "select 1 from t where (a, b, c, d) in ((?, ?, ?, ?), (?, ?, ?, ?))",
			args:     []any{1, 2, 3, 4, 5, 6, 7, 8},
			withArgs: "select 1 from t where (a, b, c, d) in ((1, 2, 3, 4), (5, 6, 7, 8))",
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
		{
			name:   "both kinds together: block kept, parser removed",
			tmpl:   "select /** cols */ id from t where /*%! TODO */ a = /*a*/1",
			params: map[string]any{"a": 1},
			sql:    "select /** cols */ id from t where  a = ?",
			args:   []any{1},
		},
	})
}

// Placeholder numbering must be monotonic across droppable clauses, subqueries, set
// operands, and spliced partials — index-based dialects ($n/:n/@pn) reset per child state
// if the index is derived from a per-state args length.
func TestPlaceholderNumbering(t *testing.T) {
	dialects := []struct {
		name string
		d    dialect.Dialect
		p    func(n int) string
	}{
		{"mysql", dialect.MySQL, func(int) string { return "?" }},
		{"postgres", dialect.PostgreSQL, func(n int) string { return "$" + strconv.Itoa(n) }},
		{"oracle", dialect.Oracle, func(n int) string { return ":" + strconv.Itoa(n) }},
		{"sqlserver", dialect.SQLServer, func(n int) string { return "@p" + strconv.Itoa(n) }},
	}
	cases := []struct {
		name   string
		tmpl   string
		params map[string]any
		want   func(p func(int) string) string
		args   []any
	}{
		{
			name:   "two clauses",
			tmpl:   "select /*a*/1 from t where b = /*b*/2",
			params: map[string]any{"a": 10, "b": 20},
			want:   func(p func(int) string) string { return "select " + p(1) + " from t where b = " + p(2) },
			args:   []any{10, 20},
		},
		{
			name:   "subquery",
			tmpl:   "select * from (select id from t where a = /*a*/1) x where b = /*b*/2",
			params: map[string]any{"a": 1, "b": 2},
			want: func(p func(int) string) string {
				return "select * from (select id from t where a = " + p(1) + ") x where b = " + p(2)
			},
			args: []any{1, 2},
		},
		{
			name:   "IN spans clauses",
			tmpl:   "select /*a*/1 from t where id in /*ids*/(0)",
			params: map[string]any{"a": 5, "ids": []any{7, 8, 9}},
			want: func(p func(int) string) string {
				return "select " + p(1) + " from t where id in (" + p(2) + ", " + p(3) + ", " + p(4) + ")"
			},
			args: []any{5, 7, 8, 9},
		},
		{
			name:   "set operands",
			tmpl:   "select id from a where x = /*a*/1 union select id from b where y = /*b*/2",
			params: map[string]any{"a": 1, "b": 2},
			want: func(p func(int) string) string {
				return "select id from a where x = " + p(1) + " union select id from b where y = " + p(2)
			},
			args: []any{1, 2},
		},
	}
	for _, d := range dialects {
		for _, c := range cases {
			t.Run(d.name+"/"+c.name, func(t *testing.T) {
				tmpl, err := bisql.Parse(c.tmpl, bisql.WithDialect(d.d))
				if err != nil {
					t.Fatal(err)
				}
				stmt, err := tmpl.Build(c.params)
				if err != nil {
					t.Fatal(err)
				}
				if want := c.want(d.p); stmt.SQL != want {
					t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, want)
				}
				if !reflect.DeepEqual(stmt.Args, c.args) {
					t.Errorf("Args got %#v want %#v", stmt.Args, c.args)
				}
			})
		}
	}
}

// A clause or set operand whose only content is a bind must be kept (the value is real
// content and must not be silently discarded).
func TestBindOnlyContentKept(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "bind-only union is not empty",
			tmpl:     "select /*a*/1 union select /*b*/2",
			params:   map[string]any{"a": 1, "b": 2},
			sql:      "select ? union select ?",
			args:     []any{1, 2},
			withArgs: "select 1 union select 2",
		},
		{
			name:   "bind-only order by is kept",
			tmpl:   "select * from t where a = /*a*/1 order by /*b*/2",
			params: map[string]any{"a": 1, "b": 2},
			sql:    "select * from t where a = ? order by ?",
			args:   []any{1, 2},
		},
		{
			name:   "bind-only where is kept",
			tmpl:   "select 1 from t where /*x*/0",
			params: map[string]any{"x": 5},
			sql:    "select 1 from t where ?",
			args:   []any{5},
		},
	})
}

// Promoted fields of an embedded (anonymous) struct are reachable by their bare name
// (Go field promotion / encoding-json semantics).
func TestStructParamsEmbedded(t *testing.T) {
	type Base struct{ TenantID int }
	type Query struct {
		Base
		Name string
	}
	tmpl, err := bisql.Parse("select * from t where tenant = /*TenantID*/0 and name = /*Name*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(Query{Base: Base{TenantID: 7}, Name: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "select * from t where tenant = ? and name = ?" {
		t.Errorf("SQL got %q", stmt.SQL)
	}
	if !reflect.DeepEqual(stmt.Args, []any{7, "bob"}) {
		t.Errorf("Args got %#v, want [7 bob]", stmt.Args)
	}
}

// A shallower field shadows a promoted one of the same name.
func TestStructParamsShadowing(t *testing.T) {
	type Base struct{ ID int }
	type Query struct {
		Base
		ID int // shadows Base.ID
	}
	tmpl, err := bisql.Parse("select /*ID*/0 from t")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(Query{Base: Base{ID: 1}, ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stmt.Args, []any{2}) {
		t.Errorf("Args got %#v, want [2] (outer ID shadows Base.ID)", stmt.Args)
	}
}
