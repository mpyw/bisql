// Package ast is the shallow structural tree of a SQL template. Ported from Komapper's
// SqlNode. Every node reproduces its original text via Text() (lossless); parser tests
// assert that Text() reproduces the input.
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

// Statement is the whole template, or one side of a parenthesized/set-operation subtree.
type Statement struct{ Nodes []Node }

func (n Statement) Text() string { return join(n.Nodes) }

// Set is UNION / EXCEPT / MINUS / INTERSECT.
type Set struct {
	Loc     Location
	Keyword string
	Left    Node
	Right   Node
}

func (n Set) Text() string { return n.Left.Text() + n.Keyword + n.Right.Text() }

// Paren is a parenthesized group (...).
type Paren struct{ Node Node }

func (n Paren) Text() string { return "(" + n.Node.Text() + ")" }

// ClauseKind is the kind of clause.
type ClauseKind int

const (
	ClauseSelect ClauseKind = iota
	ClauseFrom
	ClauseWhere
	ClauseHaving
	ClauseGroupBy
	ClauseOrderBy
	ClauseForUpdate
	ClauseOption
)

// Clause is a SQL clause. The renderer drops it when its body becomes empty
// (except Select/From/ForUpdate).
type Clause struct {
	Loc     Location
	Kind    ClauseKind
	Keyword string
	Nodes   []Node
}

func (n Clause) Text() string { return n.Keyword + join(n.Nodes) }

// LogicalKind is AND / OR.
type LogicalKind int

const (
	LogicalAnd LogicalKind = iota
	LogicalOr
)

// Logical is AND / OR. The renderer emits the keyword only when preceding content exists.
type Logical struct {
	Loc     Location
	Kind    LogicalKind
	Keyword string
	Nodes   []Node
}

func (n Logical) Text() string { return n.Keyword + join(n.Nodes) }

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

// WithBlock is /*%with e*/ ... /*%end*/.
type WithBlock struct {
	With WithDirective
	End  EndDirective
}

func (n WithBlock) Text() string { return n.With.Text() + n.End.Text() }

type WithDirective struct {
	Loc        Location
	Token      string
	Expression string
	Nodes      []Node
}

func (n WithDirective) Text() string { return n.Token + join(n.Nodes) }

// BindValue is /* expr */literal. Test is the test literal (a Word or Paren).
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

// EmbeddedValue is /*# expr */. bisql evaluates the expression to a string, then re-parses
// and splices it recursively (unlike Komapper's raw-text embed). The text is data, so only
// trusted values should flow through it. Its static counterpart is Partial (/*> name */).
type EmbeddedValue struct {
	Loc        Location
	Token      string
	Expression string
}

func (n EmbeddedValue) Text() string { return n.Token }

// Partial is /*> name */ (Komapper-compatible). bisql resolves it as an include: the named
// fragment is re-parsed into nodes and spliced in, so /*%if*/ and binds inside work.
type Partial struct {
	Loc   Location
	Token string
	Name  string
}

func (n Partial) Text() string { return n.Token }

// --- leaf tokens ---

type Word struct{ Token string }

func (n Word) Text() string { return n.Token }

type Other struct{ Token string }

func (n Other) Text() string { return n.Token }

type Comment struct{ Token string }

func (n Comment) Text() string { return n.Token }

// Space and Eol are blank nodes: not real SQL body. The renderer buffers them (matching on
// their concrete types) and does not count them as "available".
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
