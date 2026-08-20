package bisql_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/bindsyntax"
	"github.com/mpyw/bisql/dialect"
)

func TestWithBindSyntax_defaultIsTwoWay(t *testing.T) {
	tmpl, err := bisql.Parse("select 1 where x = /*x*/'a'")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmt, err := tmpl.Build(map[string]any{"x": "b"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "select 1 where x = ?" || len(stmt.Args) != 1 {
		t.Errorf("SQL = %q, Args = %v", stmt.SQL, stmt.Args)
	}
}

func TestSqlcNamed(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		params map[string]any
		sql    string
		args   []any
	}{
		{
			name:   "at form",
			src:    "select id from users where status = @status",
			params: map[string]any{"status": "active"},
			sql:    "select id from users where status = $1",
			args:   []any{"active"},
		},
		{
			name:   "call form, bare name",
			src:    "select id from users where status = sqlc.arg(status)",
			params: map[string]any{"status": "active"},
			sql:    "select id from users where status = $1",
			args:   []any{"active"},
		},
		{
			name:   "call form, quoted name",
			src:    "select id from users where status = sqlc.arg('status')",
			params: map[string]any{"status": "active"},
			sql:    "select id from users where status = $1",
			args:   []any{"active"},
		},
		{
			name:   "nullable form binds like any other",
			src:    "update users set note = sqlc.narg('note') where id = @id",
			params: map[string]any{"note": nil, "id": 7},
			sql:    "update users set note = $1 where id = $2",
			args:   []any{nil, 7},
		},
		{
			// A dotted name is why the call form exists; @c.name is rejected outright.
			name:   "dotted name reaches into a value",
			src:    "select id from users where name = sqlc.arg('c.name')",
			params: map[string]any{"c": map[string]any{"name": "ada"}},
			sql:    "select id from users where name = $1",
			args:   []any{"ada"},
		},
		{
			// Without a test literal to read the shape from, the slice form is what asks
			// for expansion; a plain arg binds the slice whole, as an array parameter.
			name:   "slice form expands into a list",
			src:    "select id from users where id in (sqlc.slice('ids'))",
			params: map[string]any{"ids": []any{1, 2, 3}},
			sql:    "select id from users where id in ($1, $2, $3)",
			args:   []any{1, 2, 3},
		},
		{
			name:   "arg form binds a slice as one parameter",
			src:    "select id from users where id = any(@ids)",
			params: map[string]any{"ids": []int{1, 2}},
			sql:    "select id from users where id = any($1)",
			args:   []any{[]int{1, 2}},
		},
		{
			name:   "a trailing cast is ordinary text",
			src:    "select id from users where name = sqlc.arg('name')::text",
			params: map[string]any{"name": "ada"},
			sql:    "select id from users where name = $1::text",
			args:   []any{"ada"},
		},
		{
			// The block directives are comments under either syntax, so numbering stays
			// gap-free across branches that did not render.
			name: "directives behave identically",
			src: "select id from users\nwhere 1 = 1\n" +
				"  /*%if activeOnly*/ and status = @status /*%end*/\n" +
				"  /*%if minAge != null*/ and age >= @min_age /*%end*/\n" +
				"  /*%for kw in keywords*/ and name like @kw /*%end*/",
			params: map[string]any{
				"activeOnly": false, "minAge": 20, "min_age": 20,
				"keywords": []any{"%a%", "%b%"},
			},
			sql: "select id from users\nwhere 1 = 1\n" +
				"  \n" +
				"   and age >= $1 \n" +
				"   and name like $2  and name like $3 ",
			args: []any{20, "%a%", "%b%"},
		},
		{
			// @> is an operator, not a bind: recognition stops when what follows @ cannot
			// start an identifier.
			name:   "an operator that starts with @ is left alone",
			src:    "select id from users where tags @> @tags",
			params: map[string]any{"tags": "{a}"},
			sql:    "select id from users where tags @> $1",
			args:   []any{"{a}"},
		},
		{
			name:   "a marker inside a string literal is text",
			src:    "select '@status' as lit where x = @x",
			params: map[string]any{"x": 1},
			sql:    "select '@status' as lit where x = $1",
			args:   []any{1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.src,
				bisql.WithBindSyntax(bindsyntax.SqlcNamed),
				bisql.WithDialect(dialect.PostgreSQL))
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
			if !reflect.DeepEqual(stmt.Args, c.args) {
				t.Errorf("Args\n got: %#v\nwant: %#v", stmt.Args, c.args)
			}
		})
	}
}

// The two forms that read a test literal have no meaning without one, and silently
// reinterpreting them would produce a query that runs while ignoring a value.
func TestSqlcNamed_rejectsTheTwoWayForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "two-way bind directive",
			src:  "select id from users where status = /*status*/'active'",
			want: "the two-way bind directive",
		},
		{
			name: "literal interpolation",
			src:  "select id from users limit /*^lim*/10",
			want: "literal interpolation",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := bisql.Parse(c.src, bisql.WithBindSyntax(bindsyntax.SqlcNamed))
			if err == nil {
				t.Fatalf("want an error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err, c.want)
			}
			if !strings.Contains(err.Error(), "sqlc-named") {
				t.Errorf("error = %q, want it to name the syntax", err)
			}
		})
	}
}

// A prefix that could only have been meant as a marker but cannot be one is a mistake, and
// this is the only place it can be caught: nothing downstream parses the SQL, so @c.name
// would otherwise render as "$1.name" and sqlc.arg(x) would be emitted verbatim.
func TestSqlcNamed_rejectsMalformedMarkers(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "dotted at-name",
			src:  "select id from users where name = @c.name",
			want: "dotted bind name",
		},
		{
			name: "unquoted dotted call argument",
			src:  "select id from users where name = sqlc.arg(c.name)",
			want: "has to be quoted",
		},
		{
			name: "double-quoted call argument",
			src:  `select id from users where name = sqlc.arg("x")`,
			want: "takes a name",
		},
		{
			name: "empty call argument",
			src:  "select id from users where name = sqlc.arg()",
			want: "takes a name",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := bisql.Parse(c.src,
				bisql.WithBindSyntax(bindsyntax.SqlcNamed),
				bisql.WithDialect(dialect.PostgreSQL))
			if err == nil {
				t.Fatalf("want an error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err, c.want)
			}
		})
	}
}

// A named marker is not a bind under the two-way syntax; it stays opaque text. Note that
// the reverse does not hold: a two-way directive under SqlcNamed is an error, not text.
func TestTwoWay_leavesNamedMarkersAlone(t *testing.T) {
	tmpl, err := bisql.Parse("select @status, tags @> '{a}'")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmt, err := tmpl.Build(nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "select @status, tags @> '{a}'" || len(stmt.Args) != 0 {
		t.Errorf("SQL = %q, Args = %v", stmt.SQL, stmt.Args)
	}
}

// sqlc does not support the @name shortcut for MySQL, where @name is a user variable, so
// bisql must not read one as a bind there either: a spelling one of them binds and the
// other does not is the divergence this arrangement exists to avoid. The same text under
// PostgreSQL, where sqlc does support the shortcut, is a bind.
func TestSqlcNamed_atFormFollowsTheDialect(t *testing.T) {
	const src = "select @row_number := @row_number + 1"

	tmpl, err := bisql.Parse(src,
		bisql.WithBindSyntax(bindsyntax.SqlcNamed), bisql.WithDialect(dialect.MySQL))
	if err != nil {
		t.Fatalf("parse (mysql): %v", err)
	}
	stmt, err := tmpl.Build(nil)
	if err != nil {
		t.Fatalf("build (mysql): %v", err)
	}
	if stmt.SQL != src || len(stmt.Args) != 0 {
		t.Errorf("mysql: SQL = %q, Args = %v; want the user variable left alone", stmt.SQL, stmt.Args)
	}

	tmpl, err = bisql.Parse(src,
		bisql.WithBindSyntax(bindsyntax.SqlcNamed), bisql.WithDialect(dialect.PostgreSQL))
	if err != nil {
		t.Fatalf("parse (postgres): %v", err)
	}
	stmt, err = tmpl.Build(map[string]any{"row_number": 1})
	if err != nil {
		t.Fatalf("build (postgres): %v", err)
	}
	if stmt.SQL != "select $1 := $2 + 1" {
		t.Errorf("postgres: SQL = %q, want the @name read as a bind", stmt.SQL)
	}

	// A double-@ session variable and the @> operator are names under no dialect.
	tmpl, err = bisql.Parse("select @@version, tags @> '{a}'",
		bisql.WithBindSyntax(bindsyntax.SqlcNamed), bisql.WithDialect(dialect.PostgreSQL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmt, err = tmpl.Build(nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "select @@version, tags @> '{a}'" || len(stmt.Args) != 0 {
		t.Errorf("SQL = %q, Args = %v", stmt.SQL, stmt.Args)
	}
}

// sqlc.embed selects a whole table into a nested struct; it is a result-column construct,
// not a bind, so it has to pass through untouched.
func TestSqlcNamed_leavesEmbedAlone(t *testing.T) {
	const src = "select sqlc.embed(authors), id from authors where id = @id"
	tmpl, err := bisql.Parse(src,
		bisql.WithBindSyntax(bindsyntax.SqlcNamed), bisql.WithDialect(dialect.PostgreSQL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmt, err := tmpl.Build(map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stmt.SQL != "select sqlc.embed(authors), id from authors where id = $1" {
		t.Errorf("SQL = %q, want sqlc.embed untouched", stmt.SQL)
	}
}
