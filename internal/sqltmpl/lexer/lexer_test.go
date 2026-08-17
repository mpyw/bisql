package lexer_test

import (
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/lexer"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

type tk struct {
	kind token.Kind
	text string
}

// scanAll drains the lexer into a slice of (kind, text), excluding the trailing EOF.
func scanAll(t *testing.T, src string) []tk {
	t.Helper()
	l := lexer.New(src)
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
		l := lexer.New(src)
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

// A word absorbs an adjacent quoted span: `abc'def'` is a single Word, not Word+Quote.
// An unterminated inner quote propagates the failure out of scanWord as Illegal.
func TestLexer_WordAbsorbsQuote(t *testing.T) {
	t.Run("absorbed", func(t *testing.T) {
		assertTokens(t, "abc'def'", []tk{{token.Word, "abc'def'"}})
	})
	t.Run("absorbed double quote", func(t *testing.T) {
		assertTokens(t, `abc"def"`, []tk{{token.Word, `abc"def"`}})
	})
	t.Run("unterminated inner quote", func(t *testing.T) {
		// scanQuoted fails inside scanWord, which returns Illegal for the whole word.
		assertTokens(t, "abc'def", []tk{{token.Illegal, ""}})
		l := lexer.New("abc'def")
		if l.Next() != token.Illegal {
			t.Fatalf("expected Illegal")
		}
		if l.Err() == nil {
			t.Errorf("expected Err() to be set")
		}
	})
}

// The signed-number rule exists so `3+4` and `a-1` keep the sign glued to the number
// (no operator token appears); a spaced `a - b` keeps '-' as a standalone Other.
func TestLexer_SignedNumberAdjacency(t *testing.T) {
	t.Run("3+4", func(t *testing.T) {
		assertTokens(t, "3+4", []tk{{token.Word, "3"}, {token.Word, "+4"}})
	})
	t.Run("a-1", func(t *testing.T) {
		assertTokens(t, "a-1", []tk{{token.Word, "a"}, {token.Word, "-1"}})
	})
	t.Run("spaced contrast a - b", func(t *testing.T) {
		assertTokens(t, "a - b", []tk{
			{token.Word, "a"}, {token.Space, " "}, {token.Other, "-"}, {token.Space, " "}, {token.Word, "b"},
		})
	})
}

// Each of \r\n, \r, \n yields exactly one EOL; runs of spaces are individual single-char
// Space tokens; a tab counts as a Space (isSpace accepts 0x09).
func TestLexer_EolAndSpace(t *testing.T) {
	t.Run("crlf", func(t *testing.T) {
		assertTokens(t, "a\r\nb", []tk{{token.Word, "a"}, {token.EOL, "\r\n"}, {token.Word, "b"}})
	})
	t.Run("cr", func(t *testing.T) {
		assertTokens(t, "a\rb", []tk{{token.Word, "a"}, {token.EOL, "\r"}, {token.Word, "b"}})
	})
	t.Run("lf", func(t *testing.T) {
		assertTokens(t, "a\nb", []tk{{token.Word, "a"}, {token.EOL, "\n"}, {token.Word, "b"}})
	})
	t.Run("two spaces are two tokens", func(t *testing.T) {
		assertTokens(t, "a  b", []tk{
			{token.Word, "a"}, {token.Space, " "}, {token.Space, " "}, {token.Word, "b"},
		})
	})
	t.Run("tab is a space", func(t *testing.T) {
		assertTokens(t, "a\tb", []tk{{token.Word, "a"}, {token.Space, "\t"}, {token.Word, "b"}})
	})
}

// Directive-name recognition is case-insensitive and skips spaces after '%'; near-miss
// names are rejected as unsupported directives (Illegal).
func TestLexer_DirectiveNameEdges(t *testing.T) {
	t.Run("recognized", func(t *testing.T) {
		assertTokens(t, "/*%IF a*/", []tk{{token.If, "/*%IF a*/"}})
		assertTokens(t, "/*% if a*/", []tk{{token.If, "/*% if a*/"}})
		assertTokens(t, "/*%  else */", []tk{{token.Else, "/*%  else */"}})
	})
	t.Run("boundary rejections", func(t *testing.T) {
		for _, src := range []string{"/*%ifx*/", "/*%elseifx*/", "/*%endd*/"} {
			assertTokens(t, src, []tk{{token.Illegal, ""}})
		}
	})
}

// Pin the "/*" branch boundary: only a bind expression start (space/identifier/quote right
// after "/*") is a BindValue; '#', '*', '+' openers stay plain block comments.
func TestLexer_BindVsComment(t *testing.T) {
	t.Run("bind", func(t *testing.T) {
		assertTokens(t, "/* x */", []tk{{token.BindValue, "/* x */"}})
	})
	t.Run("comment openers", func(t *testing.T) {
		assertTokens(t, "/*# x */", []tk{{token.MultiLineComment, "/*# x */"}})
		assertTokens(t, "/** x */", []tk{{token.MultiLineComment, "/** x */"}})
		assertTokens(t, "/*+ x */", []tk{{token.MultiLineComment, "/*+ x */"}})
	})
}

// Unterminated / partial forms all fail as Illegal with Err() set; a block comment that
// spans a newline advances the line counter so a following token reports the right column.
func TestLexer_UnterminatedAndLocation(t *testing.T) {
	t.Run("unterminated forms", func(t *testing.T) {
		for _, src := range []string{
			"`x",      // unterminated backtick quote
			"/*^x",    // unterminated literal directive
			"/*%if a", // valid directive, but no closing */
		} {
			l := lexer.New(src)
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
	})

	t.Run("location after multi-line block comment", func(t *testing.T) {
		// The block comment spans a newline (consumeToStarSlash counts it), so the following
		// word starts on line 2. Byte layout: "/* a\nb */x" -> 'x' at col 5 of line 2.
		l := lexer.New("/* a\nb */x")
		if k := l.Next(); k != token.BindValue {
			t.Fatalf("first token: got %d want BindValue", k)
		}
		if k := l.Next(); k != token.Word || l.Token() != "x" {
			t.Fatalf("second token: got %d %q want Word \"x\"", k, l.Token())
		}
		if got, want := l.Location(), (ast.Location{Line: 2, Column: 5}); got != want {
			t.Errorf("Location: got %+v want %+v", got, want)
		}
	})
}

// After an Illegal token the lexer is latched: further Next() calls keep returning Illegal
// with an empty Token (lexer.go Next early-return on l.err).
func TestLexer_IllegalIsIdempotent(t *testing.T) {
	l := lexer.New("'unterminated")
	if k := l.Next(); k != token.Illegal {
		t.Fatalf("first Next: got %d want Illegal", k)
	}
	firstErr := l.Err()
	if firstErr == nil {
		t.Fatalf("expected Err() after Illegal")
	}
	for i := 0; i < 3; i++ {
		if k := l.Next(); k != token.Illegal {
			t.Errorf("repeat Next[%d]: got %d want Illegal", i, k)
		}
		if l.Token() != "" {
			t.Errorf("repeat Next[%d]: token must be empty, got %q", i, l.Token())
		}
	}
	if l.Err() != firstErr {
		t.Errorf("Err() changed after repeated Next()")
	}
}
