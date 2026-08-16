package bisql_test

import (
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
		{name: "scalar", tmpl: "where name = /*name*/'x'", params: map[string]any{"name": "SCOTT"},
			sql: "where name = ?", args: []any{"SCOTT"}, withArgs: "where name = 'SCOTT'"},
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
			params: map[string]any{"name": "SCOTT"},
			sql:    "select * from t where 1 = 1 and name = ? ",
			args:   []any{"SCOTT"},
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

// for-loop builds a comma list with an anchor + /*%if x_has_next*/,/*%end*/ (no _next_*).
func TestForLoop(t *testing.T) {
	run(t, []buildCase{
		{
			name:   "or-anchored keyword list",
			tmpl:   "where 1 = 0 /*%for kw in kws*/or name like /*kw*/'x'/*%end*/",
			params: map[string]any{"kws": []any{"%a%", "%b%"}},
			sql:    "where 1 = 0 or name like ?or name like ?",
			args:   []any{"%a%", "%b%"},
		},
		{
			name:   "has_next comma list",
			tmpl:   "select /*%for c in cols*//*c*/0/*%if c_has_next*/, /*%end*//*%end*/",
			params: map[string]any{"cols": []any{1, 2, 3}},
			sql:    "select ?, ?, ?",
			args:   []any{1, 2, 3},
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
	type Base struct{ TenantID int }
	type Query struct {
		Base
		Name string
		ID   int
	}
	tmpl, err := bisql.Parse("where tenant = /*TenantID*/0 and name = /*Name*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(Query{Base: Base{TenantID: 7}, Name: "bob", ID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stmt.Args, []any{7, "bob"}) {
		t.Errorf("Args got %#v (embedded TenantID promoted?)", stmt.Args)
	}
	// pointer works too
	if _, err := tmpl.Build(&Query{Name: "x"}); err != nil {
		t.Errorf("pointer struct: %v", err)
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
		ld := bisql.NewRegistry().Register("active", "/*%if activeOnly*/retired = /*zero*/0/*%end*/")
		tmpl, err := bisql.Parse("select 1 from emp where /*%! @include active */ 1 = 1", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"activeOnly": true, "zero": 0})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select 1 from emp where retired = ? 1 = 1" || !reflect.DeepEqual(stmt.Args, []any{0}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})

	t.Run("nested + Expand dumps 2-way text", func(t *testing.T) {
		ld := bisql.NewRegistry().
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
		fsys := fstest.MapFS{"sql/active.sql": &fstest.MapFile{Data: []byte("retired = /*zero*/0")}}
		ld := bisql.NewFSLoader(fsys)
		tmpl, err := bisql.Parse("where /*%! @include sql/active.sql */", bisql.WithLoader(ld))
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"zero": 0})
		if stmt.SQL != "where retired = ?" {
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
		ld := bisql.NewRegistry().
			Register("a", "/*%! @include b */").
			Register("b", "/*%! @include a */")
		if _, err := bisql.Parse("/*%! @include a */", bisql.WithLoader(ld)); err == nil || !strings.Contains(err.Error(), "cyclic") {
			t.Errorf("expected cyclic error, got %v", err)
		}
	})

	t.Run("unknown fragment errors", func(t *testing.T) {
		if _, err := bisql.Parse("/*%! @include nope */", bisql.WithLoader(bisql.NewRegistry())); err == nil {
			t.Error("expected unknown-fragment error")
		}
	})

	t.Run("bare Parse rejects @include", func(t *testing.T) {
		if _, err := bisql.Parse("/*%! @include x */"); err == nil {
			t.Error("bare Parse should reject @include (no loader)")
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
		bisql.WithLoader(bisql.NewRegistry().Register("f", "name = /*name*/'x'")),
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
	s2, _ := t2.Build(map[string]any{"name": "SCOTT"})
	if s2.SQL != "where name = $1 1 = 1" {
		t.Errorf("t2 SQL = %q", s2.SQL)
	}
}

// SQLWithArgs is computed on demand and empty when there are no binds.
func TestSQLWithArgsLazy(t *testing.T) {
	tmpl, _ := bisql.Parse("select id from t where id in /*ids*/(0) and name = /*name*/'x'")
	stmt, err := tmpl.Build(map[string]any{"ids": []any{1, 2}, "name": "SCOTT"})
	if err != nil {
		t.Fatal(err)
	}
	if got := stmt.SQLWithArgs(); got != "select id from t where id in (1, 2) and name = 'SCOTT'" {
		t.Errorf("SQLWithArgs = %q", got)
	}
	// No binds: SQLWithArgs is just the SQL.
	plain, _ := bisql.Parse("select 1 from t")
	ps, _ := plain.Build(nil)
	if got := ps.SQLWithArgs(); got != ps.SQL {
		t.Errorf("no-bind SQLWithArgs = %q, want %q", got, ps.SQL)
	}
}

// ParseFile reads the root template from an fs.FS and, with no explicit loader, resolves its
// @include fragments from the same fs.FS. This mirrors the README synopsis.
func TestParseFile(t *testing.T) {
	fsys := fstest.MapFS{
		"employees/search.sql": {Data: []byte(
			"select emp_no, name\nfrom employees\nwhere 1 = 1\n" +
				"/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/\n" +
				"/*%! @include employees/_active.sql */\n" +
				"order by emp_no")},
		"employees/_active.sql": {Data: []byte(
			"/*%if activeOnly*/and retired = /*zero*/0/*%end*/")},
	}
	tmpl, err := bisql.ParseFile(fsys, "employees/search.sql", bisql.WithDialect(dialect.PostgreSQL))
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"name": "SCOTT", "activeOnly": true, "zero": 0})
	if err != nil {
		t.Fatal(err)
	}
	want := "select emp_no, name\nfrom employees\nwhere 1 = 1\nand name = $1\nand retired = $2\norder by emp_no"
	if stmt.SQL != want {
		t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, want)
	}
	if !reflect.DeepEqual(stmt.Args, []any{"SCOTT", 0}) {
		t.Errorf("Args = %#v", stmt.Args)
	}

	// A missing file is a read error, not a parse error.
	if _, err := bisql.ParseFile(fsys, "nope.sql"); err == nil {
		t.Error("missing file should error")
	}

	// ExpandFile returns the still-two-way text with the fragment spliced in.
	expanded, err := bisql.ExpandFile(fsys, "employees/search.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded, "/*%if activeOnly*/and retired = /*zero*/0/*%end*/") {
		t.Errorf("ExpandFile did not splice the fragment:\n%s", expanded)
	}
}
