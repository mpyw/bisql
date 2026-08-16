// Package lexer scans a SQL template into tokens. Ported from Komapper's SqlTokenizer
// (see docs/komapper-analysis.md).
//
// It does not parse SQL as a grammar: it recognizes clause keywords, logical/set
// operators, parentheses, string literals, and directive comments; everything else is a
// Word / Other / Space. Directive kind is decided by branching right after "/*".
package lexer

import (
	"fmt"
	"unicode/utf8"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// Lexer scans a SQL template and yields tokens one at a time.
type Lexer struct {
	src  string
	pos  int // scan position (byte offset)
	line int // current line (1-based), tracked as newlines are consumed

	lineStart int // byte offset of the current line start
	tokenLine int // line at the start of the current token
	tokenCol  int // column at the start of the current token

	token string
	err   error
}

// New creates a Lexer over src.
func New(src string) *Lexer {
	return &Lexer{src: src, line: 1, lineStart: 0}
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
	case '\'':
		return l.scanQuote()
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

	// Keywords (case-insensitive, must be word-terminated).
	if k, ok := l.scanKeyword(); ok {
		return k
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

func (l *Lexer) peekAt(i int) byte {
	if i < 0 || i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *Lexer) scanQuote() token.Kind {
	// Assumes src[pos] == '\''.
	l.pos++ // opening quote
	for l.pos < len(l.src) {
		if l.src[l.pos] == '\'' {
			if l.peekAt(l.pos+1) == '\'' {
				l.pos += 2 // escaped ''
				continue
			}
			l.pos++ // closing quote
			return token.Quote
		}
		l.pos++
	}
	return l.fail("unterminated string literal")
}

// scanSlashStar handles everything beginning with "/*": directives and plain block
// comments. On entry src[pos:pos+2] == "/*".
func (l *Lexer) scanSlashStar() token.Kind {
	var kind token.Kind
	c := l.peekAt(l.pos + 2)
	switch {
	case c == '^':
		kind = token.LiteralValue
	case c == '#':
		kind = token.EmbeddedValue
	case c == '>':
		kind = token.Partial
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
	case "with":
		return token.With, true
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

// scanKeyword tries the SQL keywords, longest first, requiring word termination.
// It returns ok=false if none match (pos unchanged).
func (l *Lexer) scanKeyword() (token.Kind, bool) {
	// multi-word first
	if n, ok := l.matchWords("for", "update"); ok {
		l.pos += n
		return token.ForUpdate, true
	}
	if n, ok := l.matchWords("group", "by"); ok {
		l.pos += n
		return token.GroupBy, true
	}
	if n, ok := l.matchWords("order", "by"); ok {
		l.pos += n
		return token.OrderBy, true
	}
	// option ( -- keyword only when followed by space(s)? Komapper requires exactly
	// "option" + one space + "(". Token is just "option".
	if n, ok := l.matchOption(); ok {
		l.pos += n
		return token.Option, true
	}
	for _, kw := range []struct {
		word string
		kind token.Kind
	}{
		{"intersect", token.Intersect},
		{"select", token.Select},
		{"having", token.Having},
		{"except", token.Except},
		{"where", token.Where},
		{"union", token.Union},
		{"minus", token.Minus},
		{"from", token.From},
		{"and", token.And},
		{"or", token.Or},
	} {
		if l.matchWord(kw.word) {
			l.pos += len(kw.word)
			return kw.kind, true
		}
	}
	return token.EOF, false
}

// matchWord reports whether src[pos:] case-insensitively starts with word and is
// word-terminated.
func (l *Lexer) matchWord(word string) bool {
	if !hasFoldPrefix(l.src, l.pos, word) {
		return false
	}
	return wordTerminated(l.src, l.pos+len(word))
}

// matchWords matches "a<space>b" where the space is a single space char; returns the total
// byte length consumed.
func (l *Lexer) matchWords(a, b string) (int, bool) {
	p := l.pos
	if !hasFoldPrefix(l.src, p, a) {
		return 0, false
	}
	p += len(a)
	if p >= len(l.src) || !isSpace(l.src[p]) {
		return 0, false
	}
	p++
	if !hasFoldPrefix(l.src, p, b) {
		return 0, false
	}
	end := p + len(b)
	if !wordTerminated(l.src, end) {
		return 0, false
	}
	return end - l.pos, true
}

// matchOption matches "option" + one space + "(", consuming only "option".
func (l *Lexer) matchOption() (int, bool) {
	p := l.pos
	if !hasFoldPrefix(l.src, p, "option") {
		return 0, false
	}
	q := p + len("option")
	if q >= len(l.src) || !isSpace(l.src[q]) {
		return 0, false
	}
	if q+1 >= len(l.src) || l.src[q+1] != '(' {
		return 0, false
	}
	return len("option"), true
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
		if c == '\'' {
			if k := l.scanQuote(); k == token.Illegal {
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
	case '=', '<', '>', '-', ',', '/', '*', '+', '(', ')', ';':
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

// wordTerminated reports whether the position i is at a word boundary (end of input or a
// non-word-part char).
func wordTerminated(src string, i int) bool {
	if i >= len(src) {
		return true
	}
	return !isWordPart(src[i])
}

// hasFoldPrefix reports whether src[pos:] starts with prefix, ASCII-case-insensitively.
func hasFoldPrefix(src string, pos int, prefix string) bool {
	if pos+len(prefix) > len(src) {
		return false
	}
	for j := 0; j < len(prefix); j++ {
		if lowerASCII(src[pos+j]) != lowerASCII(prefix[j]) {
			return false
		}
	}
	return true
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
