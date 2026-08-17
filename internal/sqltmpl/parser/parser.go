// Package parser builds the template tree from a SQL template using a reducer-stack
// strategy for the block directives (if/for) and the bind/literal test literals.
//
// It does not parse SQL as a grammar and recognizes no clauses or connectors: everything
// that is not a directive or comment is opaque text (Word / Other / Space / Eol / Paren).
package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/lexer"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// Parse turns a template string into the template tree. The result satisfies
// node.Text() == src, except that parser-level comments (/*%! ... */) and a trailing
// delimiter (;) are dropped.
func Parse(src string) (ast.Node, error) {
	p := &parser{lex: lexer.New(src)}
	p.push(&statementReducer{})
	node, err := p.parse()
	if err != nil {
		return nil, err
	}
	if p.stop == token.CloseParen {
		return nil, p.errf("unexpected close paren")
	}
	return node, nil
}

type parser struct {
	lex      *lexer.Lexer
	reducers []reducer
	stop     token.Kind // why the parse loop ended: EOF, Delimiter, or CloseParen
	loc      ast.Location
	tok      string
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("bisql/parser: "+format+" at line %d, column %d",
		append(args, p.loc.Line, p.loc.Column)...)
}

func (p *parser) push(r reducer) { p.reducers = append(p.reducers, r) }
func (p *parser) top() reducer   { return p.reducers[len(p.reducers)-1] }
func (p *parser) empty() bool    { return len(p.reducers) == 0 }
func (p *parser) pop() reducer {
	r := p.reducers[len(p.reducers)-1]
	p.reducers = p.reducers[:len(p.reducers)-1]
	return r
}

func (p *parser) pushNode(n ast.Node) {
	if !p.empty() {
		p.top().add(n)
	}
}

func (p *parser) parse() (ast.Node, error) {
	for {
		k := p.lex.Next()
		p.tok = p.lex.Token()
		p.loc = p.lex.Location()
		switch k {
		case token.Illegal:
			return nil, p.lex.Err()
		case token.EOF, token.Delimiter:
			p.stop = k
			return p.reduceAll()
		case token.CloseParen:
			p.stop = k
			return p.reduceAll()
		case token.OpenParen:
			child := &parser{lex: p.lex}
			child.push(&statementReducer{})
			node, err := child.parse()
			if err != nil {
				return nil, err
			}
			if child.stop != token.CloseParen {
				return nil, p.errf("close paren is not found")
			}
			p.pushNode(ast.Paren{Node: node})
		case token.Word, token.Quote:
			p.pushNode(ast.Word{Token: p.tok})
		case token.Space:
			p.pushNode(ast.Space{Token: p.tok})
		case token.Other:
			p.pushNode(ast.Other{Token: p.tok})
		case token.EOL:
			p.pushNode(ast.Eol{Token: p.tok})
		case token.MultiLineComment, token.SingleLineComment:
			p.pushNode(ast.Comment{Token: p.tok})
		case token.BindValue:
			if err := p.parseBind(); err != nil {
				return nil, err
			}
		case token.LiteralValue:
			if err := p.parseLiteral(); err != nil {
				return nil, err
			}
		case token.If:
			if err := p.parseIf(); err != nil {
				return nil, err
			}
		case token.Elseif:
			if err := p.parseElseif(); err != nil {
				return nil, err
			}
		case token.Else:
			if err := p.parseElse(); err != nil {
				return nil, err
			}
		case token.End:
			if err := p.parseEnd(); err != nil {
				return nil, err
			}
		case token.For:
			if err := p.parseFor(); err != nil {
				return nil, err
			}
		case token.ParserComment:
			// dropped
		}
	}
}

func (p *parser) parseBind() error {
	expr := strip(p.tok, "/*", "*/")
	if expr == "" {
		return p.errf("expression is not found in the bind value directive")
	}
	p.push(&bindReducer{loc: p.loc, token: p.tok, expr: expr})
	return nil
}

func (p *parser) parseLiteral() error {
	expr := strip(p.tok, "/*^", "*/")
	if expr == "" {
		return p.errf("expression is not found in the literal value directive")
	}
	p.push(&literalReducer{loc: p.loc, token: p.tok, expr: expr})
	return nil
}

func (p *parser) parseIf() error {
	expr := strings.TrimSpace(strings.TrimPrefix(strip(p.tok, "/*%", "*/"), "if"))
	if expr == "" {
		return p.errf("expression is not found in the if directive")
	}
	p.push(&ifBlockReducer{loc: p.loc})
	p.push(&ifDirectiveReducer{loc: p.loc, token: p.tok, expr: expr})
	return nil
}

func (p *parser) parseElseif() error {
	expr := strings.TrimSpace(strings.TrimPrefix(strip(p.tok, "/*%", "*/"), "elseif"))
	if expr == "" {
		return p.errf("expression is not found in the elseif directive")
	}
	if err := p.reduceUntil(isIfBlock); err != nil {
		return err
	}
	if p.empty() {
		return p.errf("the corresponding if directive is not found")
	}
	p.push(&elseifDirectiveReducer{loc: p.loc, token: p.tok, expr: expr})
	return nil
}

func (p *parser) parseElse() error {
	if err := p.reduceUntil(isIfBlock); err != nil {
		return err
	}
	if p.empty() {
		return p.errf("the corresponding if directive is not found")
	}
	p.push(&elseDirectiveReducer{loc: p.loc, token: p.tok})
	return nil
}

func (p *parser) parseEnd() error {
	if err := p.reduceUntil(isBlock); err != nil {
		return err
	}
	if p.empty() {
		return p.errf("the corresponding if or for directive is not found")
	}
	p.pushNode(ast.EndDirective{Loc: p.loc, Token: p.tok})
	block := p.pop()
	node, err := block.reduce()
	if err != nil {
		return err
	}
	p.pushNode(node)
	return nil
}

var inKeyword = regexp.MustCompile(`\bin\b`)

func (p *parser) parseFor() error {
	stmt := strings.TrimSpace(strings.TrimPrefix(strip(p.tok, "/*%", "*/"), "for"))
	if stmt == "" {
		return p.errf("the statement is not found in the for directive")
	}
	m := inKeyword.FindStringIndex(stmt)
	if m == nil {
		return p.errf("the keyword \"in\" is not found in the for directive")
	}
	id := strings.TrimSpace(stmt[:m[0]])
	if id == "" {
		return p.errf("the identifier is not found in the for directive")
	}
	expr := strings.TrimSpace(stmt[m[1]:])
	if expr == "" {
		return p.errf("the iterable expression is not found in the for directive")
	}
	iter, sep, err := splitForSeparator(expr)
	if err != nil {
		return p.errf("%s", err)
	}
	if iter == "" {
		return p.errf("the iterable expression is not found in the for directive")
	}
	p.push(&forBlockReducer{loc: p.loc})
	p.push(&forDirectiveReducer{loc: p.loc, token: p.tok, id: id, expr: iter, sep: sep})
	return nil
}

// splitForSeparator splits an optional trailing `: '<sep>'` separator clause off the for
// iterable expression. The separator is a constant string literal — not an expression: it is
// emitted verbatim between iterations, so allowing a runtime value would be raw-text
// injection. It is single- or double-quoted, with the quote doubled to escape it (” or "").
//
// The clause is introduced by a colon at the top level of the expression. Colons inside
// strings and inside (), [], or {} are ignored, so the expression's own uses of ':' (a
// ternary a ? b : c, a slice x[1:2], a map {k: v}) do not trigger it; parenthesize such an
// expression if it would otherwise expose a top-level colon. When a top-level colon is
// present but its tail is not a well-formed quoted literal, that is an error (rather than a
// silently mis-parsed iterable).
func splitForSeparator(expr string) (iter, sep string, err error) {
	colon := -1
	depth := 0
	var quote byte // 0 when outside a string, else the open quote (' or ")
scan:
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(expr) && expr[i+1] == quote {
					i++ // doubled-quote escape
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				colon = i
				break scan
			}
		}
	}
	if colon < 0 {
		return expr, "", nil
	}
	tail := strings.TrimSpace(expr[colon+1:])
	s, ok := unquote(tail)
	if !ok {
		return "", "", fmt.Errorf("the for separator must be a quoted string literal, got %q", tail)
	}
	return strings.TrimSpace(expr[:colon]), s, nil
}

// unquote unquotes a single- or double-quoted string literal, with the quote doubled to
// escape it (” or ""). It reports false when s is not exactly one complete such literal.
func unquote(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	q := s[0]
	if q != '\'' && q != '"' {
		return "", false
	}
	if s[len(s)-1] != q {
		return "", false
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == q {
			if i+1 < len(inner) && inner[i+1] == q {
				b.WriteByte(q)
				i++
				continue
			}
			return "", false // an unescaped quote ends the literal early
		}
		b.WriteByte(inner[i])
	}
	return b.String(), true
}

func (p *parser) reduceUntil(pred func(reducer) bool) error {
	for !p.empty() && !pred(p.top()) {
		r := p.pop()
		n, err := r.reduce()
		if err != nil {
			return err
		}
		p.pushNode(n)
	}
	return nil
}

func (p *parser) reduceAll() (ast.Node, error) {
	var node ast.Node
	for !p.empty() {
		r := p.pop()
		n, err := r.reduce()
		if err != nil {
			return nil, err
		}
		node = n
		p.pushNode(n)
	}
	if node == nil {
		return ast.Statement{}, nil
	}
	return node, nil
}

// strip removes prefix and suffix (by length) and trims surrounding whitespace.
func strip(s, prefix, suffix string) string {
	if len(s) < len(prefix)+len(suffix) {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[len(prefix) : len(s)-len(suffix)])
}
