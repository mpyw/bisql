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
		out = append(out, tk{k, l.Token()})
	}
}

func assertTokens(t *testing.T, src string, want []tk) {
	t.Helper()
	got := scanAll(t, src)
	if len(got) != len(want) {
		t.Fatalf("token count: got %d want %d\n got: %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d]: got {%d %q} want {%d %q}", i, got[i].kind, got[i].text, want[i].kind, want[i].text)
		}
	}
}

// Cases ported from Komapper's SqlTokenizerTest.

func TestLexer_EOF(t *testing.T) {
	assertTokens(t, "where", []tk{{token.Where, "where"}})
	// Calling Next past EOF keeps returning EOF with empty token.
	l := New("where")
	l.Next()
	if l.Next() != token.EOF || l.Token() != "" {
		t.Fatal("expected stable EOF")
	}
	if l.Next() != token.EOF {
		t.Fatal("EOF must stay EOF")
	}
}

func TestLexer_Empty(t *testing.T) {
	assertTokens(t, "", nil)
}

func TestLexer_Delimiter(t *testing.T) {
	assertTokens(t, "where;", []tk{{token.Where, "where"}, {token.Delimiter, ";"}})
}

func TestLexer_LineComment(t *testing.T) {
	assertTokens(t, "where--aaa\r\nbbb", []tk{
		{token.Where, "where"},
		{token.SingleLineComment, "--aaa"},
		{token.EOL, "\r\n"},
		{token.Word, "bbb"},
	})
}

func TestLexer_BlockComment(t *testing.T) {
	assertTokens(t, "where /*+aaa*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.MultiLineComment, "/*+aaa*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_BlockComment_empty(t *testing.T) {
	assertTokens(t, "where /**/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.MultiLineComment, "/**/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_ParserLevelComment(t *testing.T) {
	assertTokens(t, "where /*%!aaa*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.ParserComment, "/*%!aaa*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_Quote(t *testing.T) {
	assertTokens(t, "where 'aaa'", []tk{
		{token.Where, "where"}, {token.Space, " "}, {token.Quote, "'aaa'"},
	})
}

func TestLexer_Quote_escaped(t *testing.T) {
	assertTokens(t, "where 'aaa'''", []tk{
		{token.Where, "where"}, {token.Space, " "}, {token.Quote, "'aaa'''"},
	})
}

func TestLexer_Quote_notClosed(t *testing.T) {
	l := New("where 'aaa")
	l.Next() // where
	l.Next() // space
	if k := l.Next(); k != token.Illegal {
		t.Fatalf("expected Illegal, got %d", k)
	}
	if l.Err() == nil {
		t.Fatal("expected an error")
	}
}

func TestLexer_SetOperators(t *testing.T) {
	assertTokens(t, "union", []tk{{token.Union, "union"}})
	assertTokens(t, "except", []tk{{token.Except, "except"}})
	assertTokens(t, "minus", []tk{{token.Minus, "minus"}})
	assertTokens(t, "intersect", []tk{{token.Intersect, "intersect"}})
}

func TestLexer_Clauses(t *testing.T) {
	assertTokens(t, "select", []tk{{token.Select, "select"}})
	assertTokens(t, "from", []tk{{token.From, "from"}})
	assertTokens(t, "where", []tk{{token.Where, "where"}})
	assertTokens(t, "group by", []tk{{token.GroupBy, "group by"}})
	assertTokens(t, "having", []tk{{token.Having, "having"}})
	assertTokens(t, "order by", []tk{{token.OrderBy, "order by"}})
	assertTokens(t, "for update", []tk{{token.ForUpdate, "for update"}})
}

func TestLexer_Option(t *testing.T) {
	assertTokens(t, "option (", []tk{
		{token.Option, "option"}, {token.Space, " "}, {token.OpenParen, "("},
	})
}

func TestLexer_AndOr(t *testing.T) {
	assertTokens(t, "and", []tk{{token.And, "and"}})
	assertTokens(t, "or", []tk{{token.Or, "or"}})
}

// A keyword must be word-terminated; otherwise it is a plain word.
func TestLexer_KeywordNotTerminated(t *testing.T) {
	assertTokens(t, "wheres", []tk{{token.Word, "wheres"}})
	assertTokens(t, "ands", []tk{{token.Word, "ands"}})
}

func TestLexer_BindDirective(t *testing.T) {
	assertTokens(t, "where /*aaa*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, "/*aaa*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_BindDirective_followingQuote(t *testing.T) {
	assertTokens(t, "where /*aaa*/'2001-01-01 12:34:56'", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, "/*aaa*/"},
		{token.Quote, "'2001-01-01 12:34:56'"},
	})
}

func TestLexer_BindDirective_wordThenQuote(t *testing.T) {
	assertTokens(t, "where /*aaa*/timestamp'2001-01-01 12:34:56' and", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, "/*aaa*/"},
		{token.Word, "timestamp'2001-01-01 12:34:56'"},
		{token.Space, " "},
		{token.And, "and"},
	})
}

func TestLexer_BindDirective_spaceIncluded(t *testing.T) {
	assertTokens(t, "where /* aaa */bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, "/* aaa */"},
		{token.Word, "bbb"},
	})
}

func TestLexer_BindDirective_startsWithStringLiteral(t *testing.T) {
	assertTokens(t, `where /*"aaa"*/bbb`, []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, `/*"aaa"*/`},
		{token.Word, "bbb"},
	})
}

func TestLexer_BindDirective_startsWithCharLiteral(t *testing.T) {
	assertTokens(t, "where /*'a'*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.BindValue, "/*'a'*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_LiteralDirective(t *testing.T) {
	assertTokens(t, "where /*^aaa*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.LiteralValue, "/*^aaa*/"},
		{token.Word, "bbb"},
	})
	assertTokens(t, "where /*^ aaa */bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.LiteralValue, "/*^ aaa */"},
		{token.Word, "bbb"},
	})
}

func TestLexer_EmbeddedDirective(t *testing.T) {
	assertTokens(t, "where age > 1 /*# orderBy */", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.Word, "age"}, {token.Space, " "},
		{token.Other, ">"}, {token.Space, " "},
		{token.Word, "1"}, {token.Space, " "},
		{token.EmbeddedValue, "/*# orderBy */"},
	})
}

func TestLexer_IfDirective(t *testing.T) {
	assertTokens(t, "where /*%if true*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.If, "/*%if true*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_ForDirective(t *testing.T) {
	assertTokens(t, "where /*%for e in list*/bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.For, "/*%for e in list*/"},
		{token.Word, "bbb"},
	})
}

func TestLexer_WithDirective(t *testing.T) {
	assertTokens(t, "where /*%with element */bbb", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.With, "/*%with element */"},
		{token.Word, "bbb"},
	})
}

func TestLexer_EndDirective(t *testing.T) {
	assertTokens(t, "where bbb/*%end*/", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.Word, "bbb"},
		{token.End, "/*%end*/"},
	})
}

func TestLexer_PartialDirective(t *testing.T) {
	assertTokens(t, "where bbb/*> orderBy */", []tk{
		{token.Where, "where"}, {token.Space, " "},
		{token.Word, "bbb"},
		{token.Partial, "/*> orderBy */"},
	})
}

func TestLexer_ElseElseif(t *testing.T) {
	assertTokens(t, "/*%if a*/x/*%elseif b*/y/*%else*/z/*%end*/", []tk{
		{token.If, "/*%if a*/"}, {token.Word, "x"},
		{token.Elseif, "/*%elseif b*/"}, {token.Word, "y"},
		{token.Else, "/*%else*/"}, {token.Word, "z"},
		{token.End, "/*%end*/"},
	})
}

func TestLexer_IllegalDirective(t *testing.T) {
	l := New("where /*%*/bbb")
	l.Next() // where
	l.Next() // space
	if k := l.Next(); k != token.Illegal {
		t.Fatalf("expected Illegal, got %d", k)
	}
	if l.Err() == nil {
		t.Fatal("expected an error")
	}
}

func TestLexer_MultiLineComment_notClosed(t *testing.T) {
	l := New("where /* aaa")
	l.Next() // where
	l.Next() // space
	if k := l.Next(); k != token.Illegal {
		t.Fatalf("expected Illegal, got %d", k)
	}
	if l.Err() == nil {
		t.Fatal("expected an error")
	}
}

// Location: line/column at the start of each token (1-based). This is bisql's own
// convention (Komapper reports the post-consume position); we prefer start-of-token.
func TestLexer_Location(t *testing.T) {
	l := New("aaa bbb\nc")
	type lc struct{ line, col int }
	var got []lc
	for {
		k := l.Next()
		if k == token.EOF {
			break
		}
		loc := l.Location()
		got = append(got, lc{loc.Line, loc.Column})
	}
	want := []lc{
		{1, 1}, // aaa
		{1, 4}, // space
		{1, 5}, // bbb
		{1, 8}, // \n
		{2, 1}, // c
	}
	if len(got) != len(want) {
		t.Fatalf("count got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("loc[%d] got %v want %v", i, got[i], want[i])
		}
	}
}
