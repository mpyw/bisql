package bisql_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
	"github.com/mpyw/bisql/expr"
)

// buildCase is a full-pipeline expectation.
type buildCase struct {
	name     string
	tmpl     string
	opts     []bisql.Option
	params   map[string]any
	sql      string
	args     []any
	withArgs string // checked only when non-empty
}

func run(t *testing.T, cases []buildCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl, c.opts...)
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
			if c.withArgs != "" && stmt.SQLWithArgs() != c.withArgs {
				t.Errorf("SQLWithArgs\n got: %q\nwant: %q", stmt.SQLWithArgs(), c.withArgs)
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

func TestBind(t *testing.T) {
	run(t, []buildCase{
		{name: "scalar", tmpl: "where name = /*name*/'x'", params: map[string]any{"name": "Alice"},
			sql: "where name = ?", args: []any{"Alice"}, withArgs: "where name = 'Alice'"},
		{name: "nil value", tmpl: "where name = /*name*/'x'", params: map[string]any{"name": nil},
			sql: "where name = ?", args: []any{nil}, withArgs: "where name = null"},
		{name: "in expands", tmpl: "where id in /*ids*/('a')", params: map[string]any{"ids": []any{"x", "y", "z"}},
			sql: "where id in (?, ?, ?)", args: []any{"x", "y", "z"}, withArgs: "where id in ('x', 'y', 'z')"},
		{name: "in empty -> (null)", tmpl: "where id in /*ids*/('a')", params: map[string]any{"ids": []any{}},
			sql: "where id in (null)"},
		{name: "tuple in", tmpl: "where (a,b) in /*p*/((0,0))", params: map[string]any{"p": []any{[]any{1, 2}, []any{3, 4}}},
			sql: "where (a,b) in ((?, ?), (?, ?))", args: []any{1, 2, 3, 4}},
	})
}

// A scalar test literal binds the value as-is; a slice becomes ONE array parameter, which
// is how Postgres `= ANY($1::type[])` is expressed.
func TestArrayBind(t *testing.T) {
	pg := []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}
	tmpl, err := bisql.Parse("where ts = ANY(/*ts*/'{}'::timestamptz[])", pg...)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"ts": []any{"2020-01-01", "2020-02-02"}})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "where ts = ANY($1::timestamptz[])" {
		t.Errorf("SQL got %q", stmt.SQL)
	}
	if len(stmt.Args) != 1 || !reflect.DeepEqual(stmt.Args[0], []any{"2020-01-01", "2020-02-02"}) {
		t.Errorf("Args got %#v (want a single array arg)", stmt.Args)
	}
	// The values-embedded review form renders the array as a PostgreSQL array literal — valid
	// PostgreSQL, not a Go %v dump.
	if got, want := stmt.SQLWithArgs(), `where ts = ANY('{"2020-01-01","2020-02-02"}'::timestamptz[])`; got != want {
		t.Errorf("SQLWithArgs\n got: %q\nwant: %q", got, want)
	}
}

// When the dialect's literal formatter cannot format a bound value (here MySQL's default
// FormatLiteral facing a slice), SQLWithArgs falls back to Go's %v dump rather than failing.
// The bind is a scalar-shaped placeholder, so the whole slice binds as one argument.
func TestSQLWithArgsFallback(t *testing.T) {
	run(t, []buildCase{
		{
			name:     "mysql slice value falls back to %v",
			tmpl:     "where ts = ANY(/*ts*/'{}'::x[])",
			params:   map[string]any{"ts": []any{"a", "b"}},
			sql:      "where ts = ANY(?::x[])",
			args:     []any{[]any{"a", "b"}},
			withArgs: "where ts = ANY([a b]::x[])",
		},
	})
}

func TestLiteral(t *testing.T) {
	run(t, []buildCase{
		{name: "int", tmpl: "where a = /*^v*/'x' and b > 1", params: map[string]any{"v": 42}, sql: "where a = 42 and b > 1"},
		{name: "string escaped", tmpl: "where a = /*^v*/'x'", params: map[string]any{"v": "o'b"}, sql: "where a = 'o''b'"},
		{name: "nil", tmpl: "where a = /*^v*/'x'", params: map[string]any{"v": nil}, sql: "where a = null"},
	})
}

func TestIfElseifElse(t *testing.T) {
	const tmpl = "select 1 where /*%if a == 1*/x = 1/*%elseif a == 2*/y = 2/*%else*/z = 3/*%end*/"
	run(t, []buildCase{
		{name: "if", tmpl: tmpl, params: map[string]any{"a": 1}, sql: "select 1 where x = 1"},
		{name: "elseif", tmpl: tmpl, params: map[string]any{"a": 2}, sql: "select 1 where y = 2"},
		{name: "else", tmpl: tmpl, params: map[string]any{"a": 9}, sql: "select 1 where z = 3"},
		{name: "nil is falsy -> else", tmpl: tmpl, params: map[string]any{}, sql: "select 1 where z = 3"},
	})
}

// Anchor idioms: the engine removes nothing, so the author writes 1=1 / trailing id, etc.
func TestAnchorIdioms(t *testing.T) {
	run(t, []buildCase{
		{
			name:   "dynamic where with 1=1 anchor",
			tmpl:   "select * from t where 1 = 1 /*%if name != null*/and name = /*name*/'x'/*%end*/ /*%if age != null*/and age > /*age*/0/*%end*/",
			params: map[string]any{"name": "Alice"},
			sql:    "select * from t where 1 = 1 and name = ? ",
			args:   []any{"Alice"},
		},
		{
			name:   "dynamic where none set keeps 1=1",
			tmpl:   "select * from t where 1 = 1 /*%if name != null*/and name = /*name*/'x'/*%end*/",
			params: map[string]any{},
			sql:    "select * from t where 1 = 1 ",
		},
		{
			name:   "dynamic order by with trailing id anchor",
			tmpl:   "select * from t order by /*%if byName*/name,/*%end*/ id",
			params: map[string]any{"byName": true},
			sql:    "select * from t order by name, id",
		},
		{
			name:   "order by none set falls back to id",
			tmpl:   "select * from t order by /*%if byName*/name,/*%end*/ id",
			params: map[string]any{},
			sql:    "select * from t order by  id",
		},
	})
}

func TestForLoop(t *testing.T) {
	run(t, []buildCase{
		{
			// An OR list: the 1 = 0 anchor absorbs the leading position and each iteration leads
			// with its own "or", so an empty list renders just the anchor.
			name:   "or-anchored keyword list",
			tmpl:   "where 1 = 0 /*%for kw in kws*/or name like /*kw*/'x'/*%end*/",
			params: map[string]any{"kws": []any{"%a%", "%b%"}},
			sql:    "where 1 = 0 or name like ?or name like ?",
			args:   []any{"%a%", "%b%"},
		},
		{
			// A comma list: a fixed first element (select 0) anchors it, and each iteration leads
			// with its own comma — the same anchor+leading-connector shape as the OR list.
			name:   "comma list anchored by a fixed first element",
			tmpl:   "select 0/*%for c in cols*/, /*c*/0/*%end*/",
			params: map[string]any{"cols": []any{1, 2, 3}},
			sql:    "select 0, ?, ?, ?",
			args:   []any{1, 2, 3},
		},
		{
			// Multi-row VALUES has no anchor position, so it is expressed as a set: a
			// WHERE 1 = 0 seed (zero rows) plus one "union all select" per row.
			name:   "multi-row insert via INSERT ... SELECT + union all",
			tmpl:   "insert into t (a, b) select 0, '' where 1 = 0/*%for e in rows*/ union all select /*e.a*/0, /*e.b*/1/*%end*/",
			params: map[string]any{"rows": []any{map[string]any{"a": 1, "b": 2}, map[string]any{"a": 3, "b": 4}}},
			sql:    "insert into t (a, b) select 0, '' where 1 = 0 union all select ?, ? union all select ?, ?",
			args:   []any{1, 2, 3, 4},
		},
		{
			name:   "empty for renders nothing",
			tmpl:   "where 1 = 0 /*%for kw in kws*/or x/*%end*/",
			params: map[string]any{},
			sql:    "where 1 = 0 ",
		},
	})
}

func TestComments(t *testing.T) {
	run(t, []buildCase{
		{name: "block comment kept", tmpl: "select 1 /** keep */ from t", sql: "select 1 /** keep */ from t"},
		{name: "hint kept", tmpl: "select /*+ MAX_EXECUTION_TIME(1) */ 1", sql: "select /*+ MAX_EXECUTION_TIME(1) */ 1"},
		{name: "parser comment removed", tmpl: "select 1 /*%! todo */ from t", sql: "select 1  from t"},
		{name: "hash is now a plain comment", tmpl: "select 1 /*# x */ from t", sql: "select 1 /*# x */ from t"},
	})
}

// Casts survive after a bind (':' is not a word char); both ::type and CAST(.. AS ..) work.
func TestCasts(t *testing.T) {
	pg := []bisql.Option{bisql.WithDialect(dialect.PostgreSQL)}
	run(t, []buildCase{
		{name: "colon cast", tmpl: "where n = /*n*/1::bigint", opts: pg, params: map[string]any{"n": 1}, sql: "where n = $1::bigint", args: []any{1}},
		{name: "cast fn", tmpl: "where c = CAST(/*ts*/'x' AS timestamptz)", opts: pg, params: map[string]any{"ts": "2020-01-01"}, sql: "where c = CAST($1 AS timestamptz)", args: []any{"2020-01-01"}},
		{name: "cast then and", tmpl: "where a = /*a*/1::int and b = /*b*/2", opts: pg, params: map[string]any{"a": 1, "b": 2}, sql: "where a = $1::int and b = $2", args: []any{1, 2}},
	})
}

func TestQuotedIdentifiers(t *testing.T) {
	run(t, []buildCase{
		{name: "double-quoted id with apostrophe", tmpl: `select "it's" from t`, sql: `select "it's" from t`},
		{name: "backtick id with slashstar", tmpl: "select `a/*b` from t", sql: "select `a/*b` from t"},
	})
}

// Placeholder numbering is a single global counter across the whole statement.
func TestPlaceholderNumbering(t *testing.T) {
	const tmpl = "select /*a*/1 from t where b = /*b*/2 and id in /*ids*/(0)"
	params := map[string]any{"a": 10, "b": 20, "ids": []any{1, 2}}
	cases := []struct {
		name string
		d    dialect.Dialect
		sql  string
	}{
		{"mysql", dialect.MySQL, "select ? from t where b = ? and id in (?, ?)"},
		{"postgres", dialect.PostgreSQL, "select $1 from t where b = $2 and id in ($3, $4)"},
		{"oracle", dialect.Oracle, "select :1 from t where b = :2 and id in (:3, :4)"},
		{"sqlserver", dialect.SQLServer, "select @p1 from t where b = @p2 and id in (@p3, @p4)"},
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

func TestStructParams(t *testing.T) {
	type Base struct{ DepartmentID int }
	type Query struct {
		Base
		Name string
		ID   int
	}
	tmpl, err := bisql.Parse("where department_id = /*DepartmentID*/0 and name = /*Name*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(Query{Base: Base{DepartmentID: 3}, Name: "Bob", ID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stmt.Args, []any{3, "Bob"}) {
		t.Errorf("Args got %#v (embedded DepartmentID promoted?)", stmt.Args)
	}
	// pointer works too
	if _, err := tmpl.Build(&Query{Name: "Bob"}); err != nil {
		t.Errorf("pointer struct: %v", err)
	}
}

// toScope accepts an expr.Scope directly (not only map[string]any or a struct), passing it
// through unchanged.
func TestScopeParams(t *testing.T) {
	tmpl, err := bisql.Parse("where id = /*id*/0")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(expr.Scope{"id": 7})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "where id = ?" || !reflect.DeepEqual(stmt.Args, []any{7}) {
		t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
	}
}

// A nil pointer as params is treated as an empty scope — no error, no panic. A template with
// no field references then builds cleanly.
func TestNilPointerParams(t *testing.T) {
	tmpl, err := bisql.Parse("select 1")
	if err != nil {
		t.Fatal(err)
	}
	var p *struct{ X int }
	stmt, err := tmpl.Build(p)
	if err != nil {
		t.Fatalf("nil pointer params should not error: %v", err)
	}
	if stmt.SQL != "select 1" || len(stmt.Args) != 0 {
		t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
	}
}

// Fields of an embedded pointer-to-struct are promoted to their bare names, just like an
// embedded value struct. A nil embedded pointer is simply skipped (no promoted fields, no
// panic).
func TestEmbeddedPointerPromotion(t *testing.T) {
	type Base struct{ Name string }
	type Row struct {
		*Base
		ID int
	}
	tmpl, err := bisql.Parse("where name = /*Name*/'?' and id = /*ID*/0")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(Row{Base: &Base{Name: "x"}, ID: 1})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !reflect.DeepEqual(stmt.Args, []any{"x", 1}) {
		t.Errorf("Args got %#v (Name promoted from *Base?)", stmt.Args)
	}
	// A nil embedded pointer must not panic; a template without the promoted field builds fine.
	plain, err := bisql.Parse("where id = /*ID*/0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Build(Row{ID: 2}); err != nil {
		t.Errorf("nil embedded pointer: %v", err)
	}
}

func TestWithEvaluator(t *testing.T) {
	ev := keyEvaluator{}
	tmpl, err := bisql.Parse("/*%if flag*/x = /*val*/0/*%end*/", bisql.WithEvaluator(ev))
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"flag": true, "val": 7})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "x = ?" || !reflect.DeepEqual(stmt.Args, []any{7}) {
		t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
	}
}

type keyEvaluator struct{}

func (keyEvaluator) Eval(e string, s expr.Scope) (any, error) { return s[e], nil }

// --- @include preprocessing ---

func TestInclude(t *testing.T) {
	t.Run("register + parse", func(t *testing.T) {
		ld := bisql.NewRegistryLoader().Register("active", "/*%if activeOnly*/status = /*status*/'active'/*%end*/")
		tmpl, err := bisql.Parse("select id from users where /*%! @include active */ 1 = 1", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"activeOnly": true, "status": "active"})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select id from users where status = ? 1 = 1" || !reflect.DeepEqual(stmt.Args, []any{"active"}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})

	t.Run("nested + Expand dumps 2-way text", func(t *testing.T) {
		ld := bisql.NewRegistryLoader().
			Register("a", "x = 1 /*%! @include b */").
			Register("b", "and y = 2")
		got, err := bisql.Expand("where /*%! @include a */", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		if got != "where x = 1 and y = 2" {
			t.Errorf("Expand got %q", got)
		}
	})

	t.Run("FSLoader", func(t *testing.T) {
		fsys := fstest.MapFS{"sql/active.sql": &fstest.MapFile{Data: []byte("status = /*status*/'active'")}}
		ld := bisql.NewFSLoader(fsys)
		tmpl, err := bisql.Parse("where /*%! @include sql/active.sql */", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"status": "active"})
		if stmt.SQL != "where status = ?" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("custom LoaderFunc", func(t *testing.T) {
		ld := bisql.LoaderFunc(func(name string) (string, error) {
			return "status = /*s*/'x'", nil
		})
		tmpl, err := bisql.Parse("where /*%! @include anything */", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"s": "ok"})
		if stmt.SQL != "where status = ?" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("cycle errors", func(t *testing.T) {
		ld := bisql.NewRegistryLoader().
			Register("a", "/*%! @include b */").
			Register("b", "/*%! @include a */")
		if _, err := bisql.Parse("/*%! @include a */", bisql.WithLoader(ld)); err == nil || !strings.Contains(err.Error(), "cyclic") {
			t.Errorf("expected cyclic error, got %v", err)
		}
	})

	t.Run("unknown fragment errors", func(t *testing.T) {
		if _, err := bisql.Parse("/*%! @include nope */", bisql.WithLoader(bisql.NewRegistryLoader())); err == nil {
			t.Error("expected unknown-fragment error")
		}
	})

	t.Run("bare Parse rejects @include", func(t *testing.T) {
		if _, err := bisql.Parse("/*%! @include x */"); err == nil {
			t.Error("bare Parse should reject @include (no loader)")
		}
	})
}

// StackedLoader tries loaders in order, falling through on "not found" and aborting on any
// other error.
func TestStackedLoader(t *testing.T) {
	pg := bisql.WithDialect(dialect.PostgreSQL)

	t.Run("falls through to a later loader", func(t *testing.T) {
		// The FS lacks "frag.sql" (fs.ErrNotExist); the registry has it — the stack uses it.
		fsys := fstest.MapFS{"other.sql": {Data: []byte("x")}}
		reg := bisql.NewRegistryLoader().Register("frag.sql", "name = /*name*/'x'")
		tmpl, err := bisql.Parse("where /*%! @include frag.sql */ 1 = 1",
			pg, bisql.WithStackedLoader(bisql.NewFSLoader(fsys), reg))
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"name": "Alice"})
		if stmt.SQL != "where name = $1 1 = 1" {
			t.Errorf("SQL = %q", stmt.SQL)
		}
	})

	t.Run("earlier loader wins", func(t *testing.T) {
		first := bisql.NewRegistryLoader().Register("f", "a = /*a*/0")
		second := bisql.NewRegistryLoader().Register("f", "b = /*b*/0")
		tmpl, err := bisql.Parse("where /*%! @include f */", pg,
			bisql.WithStackedLoader(first, second))
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"a": 1})
		if stmt.SQL != "where a = $1" {
			t.Errorf("SQL = %q", stmt.SQL)
		}
	})

	t.Run("not found in any yields ErrNotFound", func(t *testing.T) {
		_, err := bisql.Parse("/*%! @include gone */",
			bisql.WithStackedLoader(bisql.NewRegistryLoader(), bisql.NewRegistryLoader()))
		if !errors.Is(err, bisql.ErrNotFound) {
			t.Errorf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("a non-not-found error aborts without falling through", func(t *testing.T) {
		boom := errors.New("backend unavailable")
		failing := bisql.LoaderFunc(func(string) (string, error) { return "", boom })
		fallback := bisql.NewRegistryLoader().Register("f", "1 = 1")
		_, err := bisql.Parse("/*%! @include f */",
			bisql.WithStackedLoader(failing, fallback))
		if !errors.Is(err, boom) {
			t.Errorf("want the backend error, got %v", err)
		}
	})
}

// --- errors ---

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		"select * from (select 1", // unbalanced paren
		"select 1)",               // stray close paren
		"select /*%if a*/x",       // if without end
		"select /*%end*/",         // end without block
		"select /* */x",           // empty bind expr
	} {
		if _, err := bisql.Parse(src); err == nil {
			t.Errorf("expected parse error for %q", src)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	cases := []struct {
		name, tmpl, want string
		params           map[string]any
	}{
		{"if non-bool", "/*%if a*/x/*%end*/", "not a boolean", map[string]any{"a": 5}},
		{"for non-iterable", "/*%for i in a*/x/*%end*/", "not iterable", map[string]any{"a": 5}},
		{"bind eval fails", "a = /*a.b.c*/0", "evaluating", map[string]any{"a": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, err = tmpl.Build(c.params)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestInvalidParams(t *testing.T) {
	tmpl, _ := bisql.Parse("select 1")
	if _, err := tmpl.Build(42); err == nil {
		t.Error("int params should error")
	}
}

// A Parser is configured once and reused across templates; its dialect (and loader) apply to
// every Parse without repeating options.
func TestNewParserReuse(t *testing.T) {
	p := bisql.NewParser(
		bisql.WithDialect(dialect.PostgreSQL),
		bisql.WithLoader(bisql.NewRegistryLoader().Register("f", "name = /*name*/'x'")),
	)
	t1, err := p.Parse("where a = /*a*/0 and b = /*b*/0")
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := t1.Build(map[string]any{"a": 1, "b": 2})
	if s1.SQL != "where a = $1 and b = $2" { // dialect from the Parser, no per-call option
		t.Errorf("t1 SQL = %q", s1.SQL)
	}
	t2, err := p.Parse("where /*%! @include f */ 1 = 1") // loader from the Parser
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := t2.Build(map[string]any{"name": "Alice"})
	if s2.SQL != "where name = $1 1 = 1" {
		t.Errorf("t2 SQL = %q", s2.SQL)
	}
}

// SQLWithArgs is computed on demand and empty when there are no binds.
func TestSQLWithArgsLazy(t *testing.T) {
	tmpl, _ := bisql.Parse("select id from t where id in /*ids*/(0) and name = /*name*/'x'")
	stmt, err := tmpl.Build(map[string]any{"ids": []any{1, 2}, "name": "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got := stmt.SQLWithArgs(); got != "select id from t where id in (1, 2) and name = 'Alice'" {
		t.Errorf("SQLWithArgs = %q", got)
	}
	// No binds: SQLWithArgs is just the SQL.
	plain, _ := bisql.Parse("select 1 from t")
	ps, _ := plain.Build(nil)
	if got := ps.SQLWithArgs(); got != ps.SQL {
		t.Errorf("no-bind SQLWithArgs = %q, want %q", got, ps.SQL)
	}
}

// ParseFile reads the root from the fs.FS but, when the parser already has an explicit loader,
// leaves that loader in place instead of overriding it with an FSLoader over the fs.FS. Here
// the fragment lives only in the registry (not in the fs.FS), so resolution proves the explicit
// loader was used.
func TestParseFileExplicitLoaderPrecedence(t *testing.T) {
	fsys := fstest.MapFS{
		"root.sql": {Data: []byte("where 1 = 1 /*%! @include frag */")},
	}
	p := bisql.NewParser(bisql.WithLoader(bisql.NewRegistryLoader().Register("frag", "and 1 = 1")))
	tmpl, err := p.ParseFile(fsys, "root.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "where 1 = 1 and 1 = 1" || len(stmt.Args) != 0 {
		t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
	}
}

// ParseFile reads the root template from an fs.FS and, with no explicit loader, resolves its
// @include fragments from the same fs.FS. This mirrors the README synopsis.
func TestParseFile(t *testing.T) {
	fsys := fstest.MapFS{
		"users/search.sql": {Data: []byte(
			"select id, name\nfrom users\nwhere 1 = 1\n" +
				"/*%if name != null*/and name = /*name*/'Alice'/*%end*/\n" +
				"/*%! @include users/_active.sql */\n" +
				"order by id")},
		"users/_active.sql": {Data: []byte(
			"/*%if activeOnly*/and status = /*status*/'active'/*%end*/")},
	}
	tmpl, err := bisql.ParseFile(fsys, "users/search.sql", bisql.WithDialect(dialect.PostgreSQL))
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"name": "Alice", "activeOnly": true, "status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	want := "select id, name\nfrom users\nwhere 1 = 1\nand name = $1\nand status = $2\norder by id"
	if stmt.SQL != want {
		t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, want)
	}
	if !reflect.DeepEqual(stmt.Args, []any{"Alice", "active"}) {
		t.Errorf("Args = %#v", stmt.Args)
	}

	// A missing file is a read error, not a parse error.
	if _, err := bisql.ParseFile(fsys, "nope.sql"); err == nil {
		t.Error("missing file should error")
	}

	// ExpandFile returns the still-two-way text with the fragment spliced in.
	expanded, err := bisql.ExpandFile(fsys, "users/search.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded, "/*%if activeOnly*/and status = /*status*/'active'/*%end*/") {
		t.Errorf("ExpandFile did not splice the fragment:\n%s", expanded)
	}
}
