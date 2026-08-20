// Package lexer scans a SQL template into tokens.
//
// It does not parse SQL as a grammar and recognizes no clause keywords or connectors: it
// distinguishes directive comments (/* */ variants), plain comments, string literals, and
// parentheses; everything else is a Word / Other / Space / Eol. Directive kind is decided
// by branching right after "/*".
package lexer

import (
	"fmt"
	"unicode/utf8"

	"github.com/mpyw/bisql/bindsyntax"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// Lexer scans a SQL template and yields tokens one at a time.
type Lexer struct {
	src   string
	rules bindsyntax.Rules
	pos   int // scan position (byte offset)
	line  int // current line (1-based), tracked as newlines are consumed

	lineStart int // byte offset of the current line start
	tokenLine int // line at the start of the current token
	tokenCol  int // column at the start of the current token

	token string
	err   error
}

// New creates a Lexer over src using bisql's two-way bind syntax.
func New(src string) *Lexer {
	return NewWithRules(src, bindsyntax.Rules{Syntax: bindsyntax.TwoWay})
}

// NewWithRules creates a Lexer over src that recognizes the bind spellings the rules allow.
func NewWithRules(src string, rules bindsyntax.Rules) *Lexer {
	return &Lexer{src: src, rules: rules, line: 1, lineStart: 0}
}

// Token returns the string of the most recently read token.
func (l *Lexer) Token() string { return l.token }

// Err returns the error that stopped the lexer, if any (set together with token.Illegal).
func (l *Lexer) Err() error { return l.err }

// Location returns the start position of the most recently read token (1-based).
func (l *Lexer) Location() ast.Location { return ast.Location{Line: l.tokenLine, Column: l.tokenCol} }

// Next returns the next token kind and updates Token / Location.
func (l *Lexer) Next() token.Kind {
	if l.err != nil {
		l.token = ""
		return token.Illegal
	}
	l.tokenLine = l.line
	l.tokenCol = l.pos - l.lineStart + 1
	start := l.pos
	k := l.scan()
	if k == token.Illegal {
		l.token = ""
		return token.Illegal
	}
	if k == token.EOF {
		l.token = ""
		return token.EOF
	}
	l.token = l.src[start:l.pos]
	return k
}

func (l *Lexer) fail(format string, args ...any) token.Kind {
	l.err = fmt.Errorf("bisql/lexer: "+format+" at line %d, column %d", append(args, l.tokenLine, l.tokenCol)...)
	return token.Illegal
}

// scan advances past one token and returns its kind (pos is left at the token end).
func (l *Lexer) scan() token.Kind {
	if l.pos >= len(l.src) {
		return token.EOF
	}
	c := l.src[l.pos]

	// EOL: \r\n, \r, \n
	if c == '\r' || c == '\n' {
		if c == '\r' && l.peekAt(l.pos+1) == '\n' {
			l.pos += 2
		} else {
			l.pos++
		}
		l.line++
		l.lineStart = l.pos
		return token.EOL
	}

	// A single space character.
	if isSpace(c) {
		l.pos++
		return token.Space
	}

	switch c {
	case ';':
		l.pos++
		return token.Delimiter
	case '(':
		l.pos++
		return token.OpenParen
	case ')':
		l.pos++
		return token.CloseParen
	case '\'', '"', '`':
		// '...' string literal, "..." / `...` quoted identifier. All are opaque spans; we
		// only need to skip their contents so an inner ' or /* is not mis-lexed.
		return l.scanQuoted(c)
	}

	// -- line comment (before treating '-' as Other)
	if c == '-' && l.peekAt(l.pos+1) == '-' {
		l.pos += 2
		for l.pos < len(l.src) {
			b := l.src[l.pos]
			if b == '\r' || b == '\n' {
				break
			}
			l.pos++
		}
		return token.SingleLineComment
	}

	// /* ... */ directive or block comment
	if c == '/' && l.peekAt(l.pos+1) == '*' {
		return l.scanSlashStar()
	}

	// A bind spelled in the SQL rather than in a comment is opaque text, so it has to be
	// recognized before the surrounding word absorbs it. A prefix that can only have been
	// meant as a marker but cannot be one is a mistake, and failing here is the only place
	// it can be caught: bisql never parses the SQL, so nothing downstream would notice.
	{
		rest := l.src[l.pos:]
		if reason, bad := l.rules.Malformed(rest); bad {
			return l.fail("%s", reason)
		}
		if m, ok := l.rules.Recognize(rest); ok {
			l.advanceOver(m.Len)
			return token.NamedBind
		}
	}

	// A word (identifier / keyword-like / number), possibly absorbing a '...' literal.
	if isWordStart(l.src, l.pos) {
		return l.scanWord()
	}

	// Anything else is a single Other character (advance by one rune).
	_, size := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += size
	return token.Other
}

// advanceOver consumes n bytes, keeping the line and column bookkeeping honest: a bind
// marker may be written across lines (sqlc.arg(\n'x')).
func (l *Lexer) advanceOver(n int) {
	for i := 0; i < n; i++ {
		if l.src[l.pos+i] == '\n' {
			l.line++
			l.lineStart = l.pos + i + 1
		}
	}
	l.pos += n
}

func (l *Lexer) peekAt(i int) byte {
	if i < 0 || i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

// scanQuoted scans a quoted span opened by quote (', ", or `). The quote char is escaped by
// doubling it (SQL standard: ” "" “). Backslash escaping (MySQL default) is not handled;
// use doubling in templates.
func (l *Lexer) scanQuoted(quote byte) token.Kind {
	// Assumes src[pos] == quote.
	l.pos++ // opening quote
	for l.pos < len(l.src) {
		if l.src[l.pos] == quote {
			if l.peekAt(l.pos+1) == quote {
				l.pos += 2 // escaped (doubled) quote
				continue
			}
			l.pos++ // closing quote
			return token.Quote
		}
		l.pos++
	}
	return l.fail("unterminated quoted literal")
}

// scanSlashStar handles everything beginning with "/*": directives and plain block
// comments. On entry src[pos:pos+2] == "/*".
func (l *Lexer) scanSlashStar() token.Kind {
	var kind token.Kind
	c := l.peekAt(l.pos + 2)
	switch {
	case c == '^':
		kind = token.LiteralValue
	case c == '%':
		k, ok := l.directiveNameKind()
		if !ok {
			// consume to the closing */ so location is stable, then fail.
			l.consumeToStarSlash()
			return l.fail("unsupported directive")
		}
		kind = k
	case isExprIdentStart(l.src, l.pos+2):
		kind = token.BindValue
	default:
		kind = token.MultiLineComment
	}
	if !l.consumeToStarSlash() {
		return l.fail("unterminated block comment")
	}
	return kind
}

// directiveNameKind inspects a "/*% ..." directive and returns its kind. It does not move
// l.pos (uses a local cursor). ok=false means the name is unsupported.
func (l *Lexer) directiveNameKind() (token.Kind, bool) {
	i := l.pos + 3 // skip "/*%"
	for i < len(l.src) && isSpace(l.src[i]) {
		i++
	}
	if i < len(l.src) && l.src[i] == '!' {
		return token.ParserComment, true
	}
	name := directiveWord(l.src, i)
	switch name {
	case "if":
		return token.If, true
	case "elseif":
		return token.Elseif, true
	case "else":
		return token.Else, true
	case "for":
		return token.For, true
	case "end":
		return token.End, true
	default:
		return token.EOF, false
	}
}

// consumeToStarSlash advances l.pos past the next "*/", tracking newlines. Returns false if
// no closing "*/" exists. On entry l.pos is at the opening "/*".
func (l *Lexer) consumeToStarSlash() bool {
	i := l.pos + 2
	for i < len(l.src) {
		if l.src[i] == '*' && l.peekAt(i+1) == '/' {
			i += 2
			l.pos = i
			return true
		}
		if l.src[i] == '\n' {
			l.line++
			l.lineStart = i + 1
		}
		i++
	}
	l.pos = len(l.src)
	return false
}

// scanWord reads a word, absorbing an embedded '...' string literal if present. It is only
// entered when isWordStart holds, so the first byte is a valid start. A leading sign of a
// signed number (+/-) is a valid start but not itself a word-part, so it is consumed
// explicitly; otherwise the loop would break at position 0 and the caller would spin.
func (l *Lexer) scanWord() token.Kind {
	if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
		l.pos++
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\'' || c == '"' || c == '`' {
			if k := l.scanQuoted(c); k == token.Illegal {
				return token.Illegal
			}
			continue
		}
		if !isWordPart(c) {
			break
		}
		l.pos++
	}
	return token.Word
}

// --- character classes ---

func isSpace(c byte) bool {
	switch c {
	case 0x09, 0x0B, 0x0C, 0x1C, 0x1D, 0x1E, 0x1F, 0x20:
		return true
	}
	return false
}

// isWordPart reports whether c can be part of a word. Mirrors Komapper: not whitespace and
// not one of the SQL punctuation characters.
func isWordPart(c byte) bool {
	if c == '\r' || c == '\n' || isSpace(c) {
		return false
	}
	switch c {
	case '=', '<', '>', '-', ',', '/', '*', '+', '(', ')', ';', ':':
		return false
	}
	return true
}

// isWordStart reports whether a word starts at src[i]. '+'/'-' start a word only when
// followed by a digit (signed number).
func isWordStart(src string, i int) bool {
	c := src[i]
	if c == '+' || c == '-' {
		if i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' {
			return true
		}
		return false
	}
	return isWordPart(c)
}

// isExprIdentStart reports whether the char at src[i] can start a bind-directive
// expression: a letter/underscore/currency (identifier start), whitespace, or a quote.
func isExprIdentStart(src string, i int) bool {
	if i >= len(src) {
		return false
	}
	c := src[i]
	if isSpace(c) || c == '\r' || c == '\n' || c == '"' || c == '\'' {
		return true
	}
	r, _ := utf8.DecodeRuneInString(src[i:])
	return r == '_' || r == '$' || isLetter(r)
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// directiveWord reads a run of ASCII letters starting at src[i] (lowercased).
func directiveWord(src string, i int) string {
	start := i
	for i < len(src) {
		c := src[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			i++
			continue
		}
		break
	}
	// lower-case the run
	b := make([]byte, i-start)
	for j := start; j < i; j++ {
		b[j-start] = lowerASCII(src[j])
	}
	return string(b)
}
