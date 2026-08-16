package parser

import (
	"fmt"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// reducer accumulates child nodes and folds them into a single ast.Node.
// Ported from Komapper's SqlReducer hierarchy.
type reducer interface {
	add(ast.Node)
	reduce() (ast.Node, error)
}

// block marks reducers that /*%end*/ folds down to (if/for/with).
type block interface {
	reducer
	isBlock()
}

func isBlock(r reducer) bool   { _, ok := r.(block); return ok }
func isIfBlock(r reducer) bool { _, ok := r.(*ifBlockReducer); return ok }

// --- simple reducers ---

type statementReducer struct{ nodes []ast.Node }

func (r *statementReducer) add(n ast.Node)            { r.nodes = append(r.nodes, n) }
func (r *statementReducer) reduce() (ast.Node, error) { return ast.Statement{Nodes: r.nodes}, nil }

type setReducer struct {
	loc     ast.Location
	keyword string
	left    ast.Node
	nodes   []ast.Node
}

func (r *setReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *setReducer) reduce() (ast.Node, error) {
	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("bisql/parser: the right operand of a set operation is not found")
	}
	right := r.nodes[0]
	// Any trailing nodes belong to the right statement already (it was reduced whole);
	// setReducer only ever receives one statement node.
	return ast.Set{Loc: r.loc, Keyword: r.keyword, Left: r.left, Right: right}, nil
}

type clauseReducer struct {
	loc     ast.Location
	kind    ast.ClauseKind
	keyword string
	nodes   []ast.Node
}

func (r *clauseReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *clauseReducer) reduce() (ast.Node, error) {
	return ast.Clause{Loc: r.loc, Kind: r.kind, Keyword: r.keyword, Nodes: r.nodes}, nil
}

type logicalReducer struct {
	loc     ast.Location
	kind    ast.LogicalKind
	keyword string
	nodes   []ast.Node
}

func (r *logicalReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *logicalReducer) reduce() (ast.Node, error) {
	return ast.Logical{Loc: r.loc, Kind: r.kind, Keyword: r.keyword, Nodes: r.nodes}, nil
}

type bindReducer struct {
	loc   ast.Location
	token string
	expr  string
	nodes []ast.Node
}

func (r *bindReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *bindReducer) reduce() (ast.Node, error) {
	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("bisql/parser: the test value must follow the bind value directive at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	test := r.nodes[0]
	switch test.(type) {
	case ast.Word, ast.Paren:
		return ast.BindValue{Loc: r.loc, Token: r.token, Expression: r.expr, Test: test, Trailing: r.nodes[1:]}, nil
	default:
		return nil, fmt.Errorf("bisql/parser: the test value must follow the bind value directive at line %d, column %d", r.loc.Line, r.loc.Column)
	}
}

type literalReducer struct {
	loc   ast.Location
	token string
	expr  string
	nodes []ast.Node
}

func (r *literalReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *literalReducer) reduce() (ast.Node, error) {
	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("bisql/parser: the test value must follow the literal value directive at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	test := r.nodes[0]
	if _, ok := test.(ast.Word); !ok {
		return nil, fmt.Errorf("bisql/parser: the test value must follow the literal value directive at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	return ast.LiteralValue{Loc: r.loc, Token: r.token, Expression: r.expr, Test: test, Trailing: r.nodes[1:]}, nil
}

// --- directive reducers (folded into their block) ---

type ifDirectiveReducer struct {
	loc   ast.Location
	token string
	expr  string
	nodes []ast.Node
}

func (r *ifDirectiveReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *ifDirectiveReducer) reduce() (ast.Node, error) {
	return ast.IfDirective{Loc: r.loc, Token: r.token, Expression: r.expr, Nodes: r.nodes}, nil
}

type elseifDirectiveReducer struct {
	loc   ast.Location
	token string
	expr  string
	nodes []ast.Node
}

func (r *elseifDirectiveReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *elseifDirectiveReducer) reduce() (ast.Node, error) {
	return ast.ElseifDirective{Loc: r.loc, Token: r.token, Expression: r.expr, Nodes: r.nodes}, nil
}

type elseDirectiveReducer struct {
	loc   ast.Location
	token string
	nodes []ast.Node
}

func (r *elseDirectiveReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *elseDirectiveReducer) reduce() (ast.Node, error) {
	return ast.ElseDirective{Loc: r.loc, Token: r.token, Nodes: r.nodes}, nil
}

type forDirectiveReducer struct {
	loc   ast.Location
	token string
	id    string
	expr  string
	nodes []ast.Node
}

func (r *forDirectiveReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *forDirectiveReducer) reduce() (ast.Node, error) {
	return ast.ForDirective{Loc: r.loc, Token: r.token, Identifier: r.id, Expression: r.expr, Nodes: r.nodes}, nil
}

type withDirectiveReducer struct {
	loc   ast.Location
	token string
	expr  string
	nodes []ast.Node
}

func (r *withDirectiveReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *withDirectiveReducer) reduce() (ast.Node, error) {
	return ast.WithDirective{Loc: r.loc, Token: r.token, Expression: r.expr, Nodes: r.nodes}, nil
}

// --- block reducers ---

type ifBlockReducer struct {
	loc   ast.Location
	nodes []ast.Node
}

func (r *ifBlockReducer) isBlock()       {}
func (r *ifBlockReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *ifBlockReducer) reduce() (ast.Node, error) {
	var (
		ifDir  *ast.IfDirective
		elseif []ast.ElseifDirective
		elseD  *ast.ElseDirective
		endD   *ast.EndDirective
	)
	seenElse := false
	for _, n := range r.nodes {
		switch d := n.(type) {
		case ast.IfDirective:
			dd := d
			ifDir = &dd
		case ast.ElseifDirective:
			if seenElse {
				return nil, fmt.Errorf("bisql/parser: an elseif directive appears after else at line %d, column %d", d.Loc.Line, d.Loc.Column)
			}
			elseif = append(elseif, d)
		case ast.ElseDirective:
			if seenElse {
				return nil, fmt.Errorf("bisql/parser: a second else directive is found at line %d, column %d", d.Loc.Line, d.Loc.Column)
			}
			seenElse = true
			dd := d
			elseD = &dd
		case ast.EndDirective:
			dd := d
			endD = &dd
		default:
			return nil, fmt.Errorf("bisql/parser: unexpected node in if block: %T", n)
		}
	}
	if ifDir == nil {
		return nil, fmt.Errorf("bisql/parser: the if directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	if endD == nil {
		return nil, fmt.Errorf("bisql/parser: the corresponding end directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	return ast.IfBlock{If: *ifDir, Elseif: elseif, Else: elseD, End: *endD}, nil
}

type forBlockReducer struct {
	loc   ast.Location
	nodes []ast.Node
}

func (r *forBlockReducer) isBlock()       {}
func (r *forBlockReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *forBlockReducer) reduce() (ast.Node, error) {
	var forDir *ast.ForDirective
	var endD *ast.EndDirective
	for _, n := range r.nodes {
		switch d := n.(type) {
		case ast.ForDirective:
			dd := d
			forDir = &dd
		case ast.EndDirective:
			dd := d
			endD = &dd
		default:
			return nil, fmt.Errorf("bisql/parser: unexpected node in for block: %T", n)
		}
	}
	if forDir == nil {
		return nil, fmt.Errorf("bisql/parser: the for directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	if endD == nil {
		return nil, fmt.Errorf("bisql/parser: the corresponding end directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	return ast.ForBlock{For: *forDir, End: *endD}, nil
}

type withBlockReducer struct {
	loc   ast.Location
	nodes []ast.Node
}

func (r *withBlockReducer) isBlock()       {}
func (r *withBlockReducer) add(n ast.Node) { r.nodes = append(r.nodes, n) }
func (r *withBlockReducer) reduce() (ast.Node, error) {
	var withDir *ast.WithDirective
	var endD *ast.EndDirective
	for _, n := range r.nodes {
		switch d := n.(type) {
		case ast.WithDirective:
			dd := d
			withDir = &dd
		case ast.EndDirective:
			dd := d
			endD = &dd
		default:
			return nil, fmt.Errorf("bisql/parser: unexpected node in with block: %T", n)
		}
	}
	if withDir == nil {
		return nil, fmt.Errorf("bisql/parser: the with directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	if endD == nil {
		return nil, fmt.Errorf("bisql/parser: the corresponding end directive is not found at line %d, column %d", r.loc.Line, r.loc.Column)
	}
	return ast.WithBlock{With: *withDir, End: *endD}, nil
}
