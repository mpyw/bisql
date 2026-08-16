// Package token defines the token kinds of the SQL template layer.
//
// bisql does not parse SQL as a grammar. It only recognizes clause keywords,
// logical/set operators, parentheses, string literals, and directive comments;
// everything else passes through as Word / Other / Space. See docs/komapper-analysis.md.
package token

// Kind is a token kind. Ported from Komapper's SqlTokenType.
type Kind int

const (
	EOF Kind = iota
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
	BindValue     // /* expr */literal
	LiteralValue  // /*^ expr */literal
	EmbeddedValue // /*# expr */
	ParserComment // /*%! ... */
	Partial       // /*> name */ (Komapper-compatible partial; bisql uses it as include)
	If            // /*%if e*/
	Elseif        // /*%elseif e*/
	Else          // /*%else*/
	For           // /*%for x in xs*/
	With          // /*%with e*/
	End           // /*%end*/

	// clause keywords
	Select
	From
	Where
	GroupBy
	Having
	OrderBy
	ForUpdate
	Option

	// logical / set operators
	And
	Or
	Union
	Minus
	Except
	Intersect

	// opaque tokens
	Word  // identifiers, keywords, numbers: the SQL body
	Other // , = > < - + * / and other symbols
	Quote // '...' string literal
)
