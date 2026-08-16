// Package lexer scans a SQL template into tokens. Ported from Komapper's SqlTokenizer
// (see docs/komapper-analysis.md).
package lexer

import (
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/token"
)

// Lexer scans a SQL template and yields tokens one at a time.
type Lexer struct {
	src   string
	pos   int
	token string
	loc   ast.Location
}

// New creates a Lexer over src.
func New(src string) *Lexer {
	return &Lexer{src: src, loc: ast.Location{Line: 1, Column: 1}}
}

// Next returns the next token kind and updates Token.
//
// TODO(M1): implement. Detect directives by branching right after "/*", and recognize
// clause keywords, AND/OR, set operators, parentheses, and string literals.
func (l *Lexer) Next() token.Kind { return token.EOF }

// Token returns the string of the most recently read token.
func (l *Lexer) Token() string { return l.token }

// Location returns the position of the most recently read token.
func (l *Lexer) Location() ast.Location { return l.loc }
