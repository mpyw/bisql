package lexer

import (
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

type tk struct {
	kind token.Kind
	text string
}

// scanAll drains the lexer into a slice of (kind, text), excluding the trailing EOF.
func scanAll(t *testing.T, src string) []tk {
	t.Helper()
	l := New(src)
	var out []tk
	for {
		k := l.Next()
		if k == token.EOF {
			if l.Token() != "" {
				t.Fatalf("EOF token must be empty, got %q", l.Token())
			}
			return out
		}
		if k == token.Illegal {
			return append(out, tk{token.Illegal, ""})
		}
		out = append(out, tk{k, l.Token()})
	}
}

func assertTokens(t *testing.T, src string, want []tk) {
	t.Helper()
	got := scanAll(t, src)
	if len(got) != len(want) {
		t.Fatalf("token count for %q: got %d want %d\n got: %#v\nwant: %#v", src, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] of %q: got {%d %q} want {%d %q}", i, src, got[i].kind, got[i].text, want[i].kind, want[i].text)
		}
	}
}

func TestLexer_Basics(t *testing.T) {
	assertTokens(t, "", nil)
	assertTokens(t, "abc", []tk{{token.Word, "abc"}})
	assertTokens(t, "a b", []tk{{token.Word, "a"}, {token.Space, " "}, {token.Word, "b"}})
	assertTokens(t, "a\nb", []tk{{token.Word, "a"}, {token.EOL, "\n"}, {token.Word, "b"}})
	assertTokens(t, "a,b", []tk{{token.Word, "a"}, {token.Other, ","}, {token.Word, "b"}})
	assertTokens(t, "(x)", []tk{{token.OpenParen, "("}, {token.Word, "x"}, {token.CloseParen, ")"}})
	assertTokens(t, "a;b", []tk{{token.Word, "a"}, {token.Delimiter, ";"}, {token.Word, "b"}})
}

// Former SQL keywords are now just plain words (no clause/connector recognition).
func TestLexer_NoKeywords(t *testing.T) {
	for _, w := range []string{"select", "from", "where", "and", "or", "union", "order", "group"} {
		assertTokens(t, w, []tk{{token.Word, w}})
	}
	assertTokens(t, "order by", []tk{{token.Word, "order"}, {token.Space, " "}, {token.Word, "by"}})
}

func TestLexer_Quotes(t *testing.T) {
	// all three quote kinds are opaque spans; inner ' or /* is not re-lexed.
	assertTokens(t, "'a/*b'", []tk{{token.Quote, "'a/*b'"}})
	assertTokens(t, "'it''s'", []tk{{token.Quote, "'it''s'"}})
	assertTokens(t, `"it's"`, []tk{{token.Quote, `"it's"`}})
	assertTokens(t, "`a/*b`", []tk{{token.Quote, "`a/*b`"}})
	assertTokens(t, `"a""b"`, []tk{{token.Quote, `"a""b"`}})
}

func TestLexer_Comments(t *testing.T) {
	assertTokens(t, "-- hi", []tk{{token.SingleLineComment, "-- hi"}})
	assertTokens(t, "/** keep */", []tk{{token.MultiLineComment, "/** keep */"}})
	assertTokens(t, "/*+ hint */", []tk{{token.MultiLineComment, "/*+ hint */"}})
	// /*# ... */ is no longer a directive: it is an ordinary block comment now.
	assertTokens(t, "/*# x */", []tk{{token.MultiLineComment, "/*# x */"}})
}

func TestLexer_Directives(t *testing.T) {
	assertTokens(t, "/*a*/x", []tk{{token.BindValue, "/*a*/"}, {token.Word, "x"}})
	assertTokens(t, "/*^a*/x", []tk{{token.LiteralValue, "/*^a*/"}, {token.Word, "x"}})
	assertTokens(t, "/*%! note */", []tk{{token.ParserComment, "/*%! note */"}})
	assertTokens(t, "/*%if a*/", []tk{{token.If, "/*%if a*/"}})
	assertTokens(t, "/*%elseif a*/", []tk{{token.Elseif, "/*%elseif a*/"}})
	assertTokens(t, "/*%else*/", []tk{{token.Else, "/*%else*/"}})
	assertTokens(t, "/*%for x in xs*/", []tk{{token.For, "/*%for x in xs*/"}})
	assertTokens(t, "/*%with u*/", []tk{{token.With, "/*%with u*/"}})
	assertTokens(t, "/*%end*/", []tk{{token.End, "/*%end*/"}})
}

// ':' is not a word char, so a cast tokenizes separately from a preceding word/number.
func TestLexer_Cast(t *testing.T) {
	assertTokens(t, "1::bigint", []tk{
		{token.Word, "1"}, {token.Other, ":"}, {token.Other, ":"}, {token.Word, "bigint"},
	})
	assertTokens(t, "x::int", []tk{
		{token.Word, "x"}, {token.Other, ":"}, {token.Other, ":"}, {token.Word, "int"},
	})
}

func TestLexer_SignedNumber(t *testing.T) {
	assertTokens(t, "-0", []tk{{token.Word, "-0"}})
	assertTokens(t, "+5", []tk{{token.Word, "+5"}})
	assertTokens(t, "a - b", []tk{
		{token.Word, "a"}, {token.Space, " "}, {token.Other, "-"}, {token.Space, " "}, {token.Word, "b"},
	})
}

func TestLexer_Errors(t *testing.T) {
	for _, src := range []string{
		"'unterminated",
		"\"unterminated",
		"/* unterminated",
		"/*%bogus*/", // unknown %directive
	} {
		l := New(src)
		var illegal bool
		for {
			k := l.Next()
			if k == token.Illegal {
				illegal = true
				break
			}
			if k == token.EOF {
				break
			}
		}
		if !illegal {
			t.Errorf("expected Illegal for %q", src)
		}
		if l.Err() == nil {
			t.Errorf("expected Err() for %q", src)
		}
	}
}
