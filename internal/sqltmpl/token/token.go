// Package token defines the token kinds of the SQL template layer.
//
// bisql does not parse SQL as a grammar and recognizes no clause keywords or connectors:
// it removes nothing implicitly (the explicit-model design). The lexer
// only distinguishes directive comments, plain comments, string literals, parentheses
// (needed to delimit a bind directive's test value and to detect IN-list expansion), and —
// under a bind syntax that spells binds in the SQL rather than in a comment — the bind
// markers themselves; everything else passes through as Word / Other / Space / Eol.
package token

// Kind is a token kind.
type Kind int

const (
	EOF Kind = iota
	// Illegal marks a lexing error; the lexer stops and exposes the error via Err().
	Illegal
	EOL
	Space

	// separators / parens
	Delimiter // ;
	OpenParen
	CloseParen

	// comments
	SingleLineComment // -- ...
	MultiLineComment  // /* ... */ (a plain block comment, not a directive)

	// directives
	BindValue // /* expr */literal
	// NamedBind is a bind that carries its own name and needs no test literal:
	// @name, sqlc.arg('name'), sqlc.narg('name'), sqlc.slice('name'). It is opaque
	// text rather than a comment, and is only recognized under bindsyntax.SqlcNamed.
	NamedBind
	LiteralValue  // /*^ expr */literal
	ParserComment // /*%! ... */ (also hosts the @include preprocessor directive)
	If            // /*%if e*/
	Elseif        // /*%elseif e*/
	Else          // /*%else*/
	For           // /*%for x in xs*/
	End           // /*%end*/

	// opaque tokens
	Word  // identifiers, keywords, numbers: the SQL body
	Other // , = > < - + * / and other symbols
	Quote // '...' string literal
)
