package lexer_test

import (
	"testing"

	"github.com/mpyw/bisql/bindsyntax"
	"github.com/mpyw/bisql/internal/sqltmpl/lexer"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// scanAllNamed drains a lexer reading binds in sqlc's syntax.
func scanAllNamed(t *testing.T, src string) []tk {
	t.Helper()
	l := lexer.NewWithBindSyntax(src, bindsyntax.SqlcNamed)
	var out []tk
	for {
		k := l.Next()
		switch k {
		case token.EOF:
			return out
		case token.Illegal:
			return append(out, tk{token.Illegal, ""})
		}
		out = append(out, tk{k, l.Token()})
	}
}

// named returns the text of every bind marker the lexer recognized.
func named(toks []tk) []string {
	var out []string
	for _, tok := range toks {
		if tok.kind == token.NamedBind {
			out = append(out, tok.text)
		}
	}
	return out
}

func TestNamedBind(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"where s = @status", []string{"@status"}},
		{"where s = sqlc.arg('status')", []string{"sqlc.arg('status')"}},
		{"where s = sqlc.narg('note')", []string{"sqlc.narg('note')"}},
		{"where id in (sqlc.slice('ids'))", []string{"sqlc.slice('ids')"}},
		{"where a = @x and b = @y", []string{"@x", "@y"}},
		{"where n = sqlc.arg('c.name')", []string{"sqlc.arg('c.name')"}},
		// A cast follows the marker as ordinary text.
		{"where n = sqlc.arg('x')::text", []string{"sqlc.arg('x')"}},
		// Operators and MySQL variables that begin with @ are not binds.
		{"where tags @> '{a}'", nil},
		{"select @@version", nil},
		// A marker inside a quoted span is text.
		{"select '@status', \"@a\", `@b`", nil},
		{"select /* @status */ 1", nil},
		// A schema-qualified call that is not one of the three forms stays opaque.
		{"select sqlc.args('x')", nil},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got := named(scanAllNamed(t, c.src))
			if len(got) != len(c.want) {
				t.Fatalf("markers = %q, want %q", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("marker %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// Under the two-way syntax the same text is opaque, which is what keeps @> and MySQL's
// @variables usable there.
func TestNamedBindOnlyUnderSqlcNamed(t *testing.T) {
	for _, src := range []string{"where s = @status", "where s = sqlc.arg('status')"} {
		for _, tok := range scanAll(t, src) {
			if tok.kind == token.NamedBind {
				t.Errorf("scanAll(%q) produced a NamedBind (%q) under the two-way syntax", src, tok.text)
			}
		}
	}
}

// A marker may be written across lines, and skipping it must not lose the line count.
func TestNamedBindKeepsLineNumbers(t *testing.T) {
	l := lexer.NewWithBindSyntax("a\nsqlc.arg(\n'x'\n)\nb", bindsyntax.SqlcNamed)
	var lastWord ast4Loc
	for {
		k := l.Next()
		if k == token.EOF || k == token.Illegal {
			break
		}
		if k == token.Word && l.Token() == "b" {
			lastWord = ast4Loc{l.Location().Line, l.Location().Column}
		}
	}
	if lastWord.line != 5 || lastWord.col != 1 {
		t.Errorf("location of the word after the marker = %d:%d, want 5:1", lastWord.line, lastWord.col)
	}
}

type ast4Loc struct{ line, col int }
