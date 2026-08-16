package bisql_test

import (
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
)

// This file is the TDD spec for bisql. Cases mirror Komapper's TEMPLATE tests
// (docs/komapper-template-features.md); expected SQL/args are taken from there so bisql
// reproduces Komapper's behavior. Every case is skipped until its milestone lands; remove
// the t.Skip in the corresponding milestone.
//
// bisql defaults to the MySQL dialect (? placeholders). Args below are the bound values in
// order.

type spec struct {
	name   string
	tmpl   string
	params map[string]any
	sql    string
	args   []any
	// milestone gates the case; see docs/roadmap.md.
	milestone string
}

func run(t *testing.T, cases []spec) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.milestone != "M4" && c.milestone != "M5" {
				t.Skipf("pending %s", c.milestone)
			}
			tmpl, err := bisql.Parse(c.tmpl)
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
			if !reflect.DeepEqual(normArgs(stmt.Args), normArgs(c.args)) {
				t.Errorf("Args\n got: %#v\nwant: %#v", stmt.Args, c.args)
			}
		})
	}
}

func normArgs(a []any) []any {
	if len(a) == 0 {
		return nil
	}
	return a
}

// --- §1 bind value directive (M4) ---

func TestBindValue(t *testing.T) {
	run(t, []spec{
		{
			name:      "single",
			tmpl:      "select name, age from person where name = /*name*/'test' and age > 1",
			params:    map[string]any{"name": "aaa"},
			sql:       "select name, age from person where name = ? and age > 1",
			args:      []any{"aaa"},
			milestone: "M4",
		},
		{
			name:      "single null",
			tmpl:      "select name from person where name = /*name*/'test'",
			params:    map[string]any{"name": nil},
			sql:       "select name from person where name = ?",
			args:      []any{nil},
			milestone: "M4",
		},
		{
			name:      "list expands to IN",
			tmpl:      "select name from person where name in /*name*/('a', 'b')",
			params:    map[string]any{"name": []any{"x", "y", "z"}},
			sql:       "select name from person where name in (?, ?, ?)",
			args:      []any{"x", "y", "z"},
			milestone: "M4",
		},
		{
			name:      "empty list becomes (null)",
			tmpl:      "select name from person where name in /*name*/('a', 'b')",
			params:    map[string]any{"name": []any{}},
			sql:       "select name from person where name in (null)",
			args:      nil,
			milestone: "M4",
		},
		// pairs/triples (tuple IN) are covered once slices of tuples are designed for Go.
	})
}

// --- §2 literal value directive (M4) ---

func TestLiteralValue(t *testing.T) {
	run(t, []spec{
		{
			name:      "embeds as literal",
			tmpl:      "select name from person where name = /*^name*/'test' and age > 1",
			params:    map[string]any{"name": "aaa"},
			sql:       "select name from person where name = 'aaa' and age > 1",
			args:      nil,
			milestone: "M4",
		},
	})
}

// --- §3 embedded value directive (M4) ---

func TestEmbeddedValue(t *testing.T) {
	run(t, []spec{
		{
			name:      "injects clause fragment",
			tmpl:      "select name from person where age > 1 /*# orderBy */",
			params:    map[string]any{"orderBy": "order by name"},
			sql:       "select name from person where age > 1 order by name",
			args:      nil,
			milestone: "M4",
		},
		{
			name:      "empty removes the clause",
			tmpl:      "select name from person where age > 1 order by /*# orderBy */",
			params:    map[string]any{"orderBy": ""},
			sql:       "select name from person where age > 1 ",
			args:      nil,
			milestone: "M4",
		},
	})
}

// --- §6 if block + §9 clause removal + §10 dangling AND/OR (M4) ---

func TestIfBlock(t *testing.T) {
	const tmpl = "select name, age from person where /*%if name != null*/name = /*name*/'test'/*%end*/ and 1 = 1"
	run(t, []spec{
		{
			name:      "true keeps condition",
			tmpl:      tmpl,
			params:    map[string]any{"name": "aaa"},
			sql:       "select name, age from person where name = ? and 1 = 1",
			args:      []any{"aaa"},
			milestone: "M4",
		},
		{
			name:      "false strips condition, keeps 1=1",
			tmpl:      tmpl,
			params:    map[string]any{"name": nil},
			sql:       "select name, age from person where   1 = 1",
			args:      nil,
			milestone: "M4",
		},
		{
			name:      "removes empty WHERE",
			tmpl:      "select name from person where /*%if name != null*/name = /*name*/'test'/*%end*/ order by name",
			params:    map[string]any{"name": nil},
			sql:       "select name from person   order by name",
			args:      nil,
			milestone: "M4",
		},
	})
}

// --- §7 for block (M4) ---

func TestForBlock(t *testing.T) {
	run(t, []spec{
		{
			name:      "or between iterations via i_next_or",
			tmpl:      "select name from person where /*%for i in list*/age = /*i*/0 /*# i_next_or */ /*%end*/",
			params:    map[string]any{"list": []any{1, 2, 3}},
			sql:       "select name from person where age = ? or age = ? or age = ?  ",
			args:      []any{1, 2, 3},
			milestone: "M4",
		},
		{
			name:      "comma between iterations",
			tmpl:      "select name from person order by /*%for i in list*//*# i *//*# i_next_comma */ /*%end*/",
			params:    map[string]any{"list": []any{"a", "b", "c"}},
			sql:       "select name from person order by a, b, c ",
			args:      nil,
			milestone: "M4",
		},
	})
}

// --- §11 set operations / §12 parens (M2/M4) ---

func TestPassthrough(t *testing.T) {
	run(t, []spec{
		{
			name:      "union unchanged",
			tmpl:      "select name from a union select name from b",
			params:    nil,
			sql:       "select name from a union select name from b",
			args:      nil,
			milestone: "M4",
		},
		{
			name:      "subquery unchanged",
			tmpl:      "select name, age from (select * from person)",
			params:    nil,
			sql:       "select name, age from (select * from person)",
			args:      nil,
			milestone: "M4",
		},
		{
			name:      "empty parens kept",
			tmpl:      "select name from my_function()",
			params:    nil,
			sql:       "select name from my_function()",
			args:      nil,
			milestone: "M4",
		},
	})
}

// --- §4 partial (include) — bisql's differentiator (M5) ---

func TestPartialInclude(t *testing.T) {
	ld := bisql.NewLoader()
	// The fragment contains /*%if*/ and a bind: they must work after re-parsing, unlike
	// Komapper's /*# */ raw embed.
	ld.Register("active", `/*%if activeOnly*/retired = /*zero*/0/*%end*/`)
	tmpl, err := ld.Parse("select emp_no from employees where /*> active */")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"activeOnly": true, "zero": 0})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "select emp_no from employees where retired = ?" {
		t.Errorf("got %q", stmt.SQL)
	}
	if !reflect.DeepEqual(stmt.Args, []any{0}) {
		t.Errorf("got %#v", stmt.Args)
	}
}

// A partial may reference further partials: expansion is recursive.
func TestPartialRecursive(t *testing.T) {
	ld := bisql.NewLoader()
	ld.Register("cond", `age > /*minAge*/0 and /*> namePart */`)
	ld.Register("namePart", `name = /*name*/'x'`)
	tmpl, err := ld.Parse("select emp_no from employees where /*> cond */")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"minAge": 20, "name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "select emp_no from employees where age > ? and name = ?"; stmt.SQL != want {
		t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, want)
	}
	if !reflect.DeepEqual(stmt.Args, []any{20, "alice"}) {
		t.Errorf("Args got %#v", stmt.Args)
	}
}

// A cyclic partial reference is reported rather than looping forever.
func TestPartialCycle(t *testing.T) {
	ld := bisql.NewLoader()
	ld.Register("a", `x = 1 and /*> b */`)
	ld.Register("b", `y = 2 and /*> a */`)
	tmpl, err := ld.Parse("select 1 from dual where /*> a */")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Build(nil); err == nil {
		t.Fatal("expected a cyclic-reference error")
	}
}

// An embedded value is re-parsed and expanded recursively: directives/binds inside the
// runtime string are evaluated (bisql extends Komapper's raw-text /*# */ here).
func TestEmbeddedRecursive(t *testing.T) {
	tmpl, err := bisql.Parse("select emp_no from employees where /*# cond */")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{
		"cond": "name = /*name*/'x'",
		"name": "bob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "select emp_no from employees where name = ?"; stmt.SQL != want {
		t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, want)
	}
	if !reflect.DeepEqual(stmt.Args, []any{"bob"}) {
		t.Errorf("Args got %#v", stmt.Args)
	}
}
