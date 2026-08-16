// Package ast is the template tree. Since the explicit-model redesign the tree carries no
// clause/connector structure (bisql removes nothing implicitly): it is opaque text and leaf
// tokens interspersed with directives (bind, literal, if/for) and comments.
// Every node reproduces its original text via Text() (lossless); parser tests assert that
// Text() reproduces the input.
package ast

// Node is a node of the template tree.
type Node interface {
	Text() string
}

// Location is a position within the template (for error messages).
type Location struct {
	Line   int
	Column int
}

// --- structural nodes ---

// Statement is the whole template, or the contents of a parenthesized group.
type Statement struct{ Nodes []Node }

func (n Statement) Text() string { return join(n.Nodes) }

// Paren is a parenthesized group (...). It carries no special semantics; it exists so a
// bind directive's test value can be a group (which drives IN-list expansion) and so
// subqueries reproduce losslessly.
type Paren struct{ Node Node }

func (n Paren) Text() string { return "(" + n.Node.Text() + ")" }

// --- blocks and directives ---

// IfBlock is /*%if*/ ... /*%elseif*/ ... /*%else*/ ... /*%end*/.
type IfBlock struct {
	If     IfDirective
	Elseif []ElseifDirective
	Else   *ElseDirective
	End    EndDirective
}

func (n IfBlock) Text() string {
	s := n.If.Text()
	for _, e := range n.Elseif {
		s += e.Text()
	}
	if n.Else != nil {
		s += n.Else.Text()
	}
	return s + n.End.Text()
}

type IfDirective struct {
	Loc        Location
	Token      string
	Expression string
	Nodes      []Node
}

func (n IfDirective) Text() string { return n.Token + join(n.Nodes) }

type ElseifDirective struct {
	Loc        Location
	Token      string
	Expression string
	Nodes      []Node
}

func (n ElseifDirective) Text() string { return n.Token + join(n.Nodes) }

type ElseDirective struct {
	Loc   Location
	Token string
	Nodes []Node
}

func (n ElseDirective) Text() string { return n.Token + join(n.Nodes) }

type EndDirective struct {
	Loc   Location
	Token string
}

func (n EndDirective) Text() string { return n.Token }

// ForBlock is /*%for x in xs*/ ... /*%end*/.
type ForBlock struct {
	For ForDirective
	End EndDirective
}

func (n ForBlock) Text() string { return n.For.Text() + n.End.Text() }

type ForDirective struct {
	Loc        Location
	Token      string
	Identifier string
	Expression string
	Nodes      []Node
}

func (n ForDirective) Text() string { return n.Token + join(n.Nodes) }

// BindValue is /* expr */literal. Test is the test literal (a Word or Paren) that follows;
// it keeps the raw template runnable and is replaced at build time by a placeholder — or,
// when Test is a Paren, by an expanded IN list. Trailing is any content that folded in
// after the test literal (e.g. a "::cast").
type BindValue struct {
	Loc        Location
	Token      string
	Expression string
	Test       Node
	Trailing   []Node
}

func (n BindValue) Text() string { return n.Token + n.Test.Text() + join(n.Trailing) }

// LiteralValue is /*^ expr */literal.
type LiteralValue struct {
	Loc        Location
	Token      string
	Expression string
	Test       Node
	Trailing   []Node
}

func (n LiteralValue) Text() string { return n.Token + n.Test.Text() + join(n.Trailing) }

// --- leaf tokens (all opaque; emitted verbatim) ---

type Word struct{ Token string }

func (n Word) Text() string { return n.Token }

type Other struct{ Token string }

func (n Other) Text() string { return n.Token }

type Comment struct{ Token string }

func (n Comment) Text() string { return n.Token }

type Space struct{ Token string }

func (n Space) Text() string { return n.Token }

type Eol struct{ Token string }

func (n Eol) Text() string { return n.Token }

func join(nodes []Node) string {
	var b []byte
	for _, n := range nodes {
		b = append(b, n.Text()...)
	}
	return string(b)
}
