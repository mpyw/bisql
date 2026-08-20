package parser_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql/bindsyntax"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
)

// The round-trip property has to hold under either syntax: a bind that carries its own name
// has no test literal, so BindValue.Text() is the marker alone.
func TestParseWithBindSyntax_roundTrips(t *testing.T) {
	for _, src := range []string{
		"select id from users where status = @status",
		"select id from users where id in (sqlc.slice('ids'))",
		"select id from users where name = sqlc.arg('c.name')::text",
		"select id from users\nwhere 1 = 1\n  /*%if a*/ and s = @s /*%end*/\n  /*%for k in ks*/ and n like @k /*%end*/",
		"select id from users where tags @> '{a}' and @@version is not null",
	} {
		t.Run(src, func(t *testing.T) {
			node, err := parser.ParseWithBindSyntax(src, bindsyntax.SqlcNamed)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := node.Text(); got != src {
				t.Errorf("Text()\n got: %q\nwant: %q", got, src)
			}
		})
	}
}

// Without a test literal there is nothing to read the shape from, so the slice form is what
// carries the request to expand.
func TestParseWithBindSyntax_expandList(t *testing.T) {
	cases := map[string]bool{
		"where id in (sqlc.slice('ids'))": true,
		"where id = @ids":                 false,
		"where id = sqlc.arg('ids')":      false,
		"where id = sqlc.narg('ids')":     false,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			node, err := parser.ParseWithBindSyntax(src, bindsyntax.SqlcNamed)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			binds := collectBinds(node)
			if len(binds) != 1 {
				t.Fatalf("found %d binds, want one", len(binds))
			}
			if binds[0].ExpandList != want {
				t.Errorf("ExpandList = %v, want %v", binds[0].ExpandList, want)
			}
			if binds[0].Test != nil {
				t.Errorf("Test = %#v, want nil", binds[0].Test)
			}
			if binds[0].Expression != "ids" {
				t.Errorf("Expression = %q, want ids", binds[0].Expression)
			}
		})
	}
}

func TestParseWithBindSyntax_rejectsTestLiteralForms(t *testing.T) {
	cases := []struct{ src, want string }{
		{"where s = /*status*/'active'", "the two-way bind directive"},
		{"limit /*^lim*/10", "literal interpolation"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := parser.ParseWithBindSyntax(c.src, bindsyntax.SqlcNamed)
			if err == nil {
				t.Fatalf("want an error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err, c.want)
			}
		})
	}
}

// collectBinds walks the tree and returns every bind it finds.
func collectBinds(n ast.Node) []ast.BindValue {
	var out []ast.BindValue
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch v := n.(type) {
		case ast.BindValue:
			out = append(out, v)
		case ast.Statement:
			for _, c := range v.Nodes {
				walk(c)
			}
		case ast.Paren:
			walk(v.Node)
		case ast.IfBlock:
			walk(v.If)
			for _, e := range v.Elseif {
				walk(e)
			}
			if v.Else != nil {
				walk(v.Else)
			}
		case ast.IfDirective:
			for _, c := range v.Nodes {
				walk(c)
			}
		case ast.ElseifDirective:
			for _, c := range v.Nodes {
				walk(c)
			}
		case ast.ElseDirective:
			for _, c := range v.Nodes {
				walk(c)
			}
		case ast.ForBlock:
			walk(v.For)
		case ast.ForDirective:
			for _, c := range v.Nodes {
				walk(c)
			}
		}
	}
	walk(n)
	return out
}
