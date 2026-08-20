package bisql_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/bindsyntax"
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

// Selecting sqlc's syntax fails loudly. Accepting it while the lexer still
// reads only the two-way form would parse a named template as opaque text with no
// binds at all, which is worse than refusing.
func TestWithBindSyntax_sqlcNamedIsRejected(t *testing.T) {
	_, err := bisql.Parse("select 1 where x = @x", bisql.WithBindSyntax(bindsyntax.SqlcNamed))
	if err == nil {
		t.Fatal("want an error for the sqlc-named bind syntax")
	}
	if !strings.Contains(err.Error(), "not implemented yet") {
		t.Errorf("error = %q, want it to say the syntax is not implemented yet", err)
	}
}
