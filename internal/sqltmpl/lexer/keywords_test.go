package lexer

import (
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// Keywords are matched case-insensitively, but the original casing is preserved in the
// token text (the lexer returns source slices).
func TestLexer_KeywordsCaseInsensitive(t *testing.T) {
	assertTokens(t, "SELECT", []tk{{token.Select, "SELECT"}})
	assertTokens(t, "Where", []tk{{token.Where, "Where"}})
	assertTokens(t, "GROUP BY", []tk{{token.GroupBy, "GROUP BY"}})
	assertTokens(t, "Order By", []tk{{token.OrderBy, "Order By"}})
	assertTokens(t, "FOR UPDATE", []tk{{token.ForUpdate, "FOR UPDATE"}})
	assertTokens(t, "UNION", []tk{{token.Union, "UNION"}})
	assertTokens(t, "Except", []tk{{token.Except, "Except"}})
	assertTokens(t, "AND", []tk{{token.And, "AND"}})
	assertTokens(t, "Or", []tk{{token.Or, "Or"}})
}

// Directive names are also case-insensitive.
func TestLexer_DirectiveCaseInsensitive(t *testing.T) {
	assertTokens(t, "/*%IF x*/", []tk{{token.If, "/*%IF x*/"}})
	assertTokens(t, "/*%For x In xs*/", []tk{{token.For, "/*%For x In xs*/"}})
}

// A multi-word keyword requires exactly one separating space; extra whitespace makes the
// first word a plain word instead.
func TestLexer_MultiWordSingleSpace(t *testing.T) {
	assertTokens(t, "group  by", []tk{
		{token.Word, "group"}, {token.Space, " "}, {token.Space, " "}, {token.Word, "by"},
	})
}

// A multi-word keyword's second word must be word-terminated; a longer word does not match.
func TestLexer_MultiWordBoundary(t *testing.T) {
	assertTokens(t, "group bytes", []tk{
		{token.Word, "group"}, {token.Space, " "}, {token.Word, "bytes"},
	})
	assertTokens(t, "for updated", []tk{
		{token.Word, "for"}, {token.Space, " "}, {token.Word, "updated"},
	})
	// no space at all: not a keyword
	assertTokens(t, "orderby", []tk{{token.Word, "orderby"}})
	// "for" alone is not a keyword (only "for update" is)
	assertTokens(t, "for", []tk{{token.Word, "for"}})
}

// A signed number is a word starting with +/-; the sign is consumed with the digits.
// (Regression: scanWord once broke at the leading sign, spinning forever — found by fuzz.)
func TestLexer_SignedNumberWord(t *testing.T) {
	assertTokens(t, "-0", []tk{{token.Word, "-0"}})
	assertTokens(t, "+5", []tk{{token.Word, "+5"}})
	assertTokens(t, "a = /*v*/-0", []tk{
		{token.Word, "a"}, {token.Space, " "}, {token.Other, "="}, {token.Space, " "},
		{token.BindValue, "/*v*/"}, {token.Word, "-0"},
	})
	// A bare '-' not before a digit is an operator (Other), not a word.
	assertTokens(t, "a - b", []tk{
		{token.Word, "a"}, {token.Space, " "}, {token.Other, "-"}, {token.Space, " "}, {token.Word, "b"},
	})
}

// option is only a keyword when followed by " (" ; otherwise it is a plain word.
func TestLexer_OptionBoundary(t *testing.T) {
	assertTokens(t, "option x", []tk{
		{token.Word, "option"}, {token.Space, " "}, {token.Word, "x"},
	})
}
