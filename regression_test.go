package bisql_test

import (
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// Regression for the placeholder-numbering bug: index-based dialects ($n, :n, @pn) must
// number binds monotonically across droppable clauses, subqueries, set operands, and
// spliced partials. Previously each child state restarted numbering at 1, corrupting every
// non-MySQL dialect.
func TestRegressionPlaceholderNumbering(t *testing.T) {
	dialects := []struct {
		name string
		d    dialect.Dialect
		p    func(n int) string
	}{
		{"mysql", dialect.MySQL, func(int) string { return "?" }},
		{"postgres", dialect.PostgreSQL, func(n int) string { return "$" + itoa(n) }},
		{"oracle", dialect.Oracle, func(n int) string { return ":" + itoa(n) }},
		{"sqlserver", dialect.SQLServer, func(n int) string { return "@p" + itoa(n) }},
	}
	cases := []struct {
		name   string
		tmpl   string
		params map[string]any
		// sql is a template with %1..%N replaced by each dialect's placeholder for that index
		want func(p func(int) string) string
		args []any
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Regression: a clause or set operand whose only content is a bind must be kept (the value
// is not silently discarded). Previously binds did not set the available flag, so these
// dropped — a union of two bind-only selects even produced empty SQL.
func TestRegressionBindOnlyKept(t *testing.T) {
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

// Regression: promoted fields of an embedded (anonymous) struct are reachable by their bare
// name, matching Go field promotion / encoding-json. Previously toScope only saw top-level
// fields, so a promoted field silently evaluated to nil.
func TestRegressionEmbeddedStructFields(t *testing.T) {
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

// A shallower field shadows a promoted one of the same name (Go promotion semantics).
func TestRegressionEmbeddedShadowing(t *testing.T) {
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
