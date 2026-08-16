// Package render evaluates the template tree into (SQL, args). Under the explicit model it
// removes nothing implicitly: text is emitted verbatim, directives are evaluated in place,
// and the author anchors clauses (1=1 / trailing id) so no separator is ever left dangling.
package render

import (
	"fmt"
	"strings"

	"github.com/mpyw/bisql/dialect"
	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// Result is the rendered statement: the executable placeholder form (SQL) and its bind
// Args. ArgSpans[i] is the [start,end) byte range in SQL of the placeholder for Args[i]
// (they are appended together, so the slices stay aligned); callers splice literals into
// those ranges to build the values-embedded review form lazily, without a second render.
type Result struct {
	SQL      string
	Args     []any
	ArgSpans [][2]int
}

// Config carries everything the renderer needs beyond the tree and the scope.
type Config struct {
	Evaluator   expr.Evaluator
	Placeholder dialect.Placeholder
	Literal     dialect.Literal
}

// Render evaluates the tree against the scope.
func Render(n ast.Node, scope expr.Scope, cfg Config) (Result, error) {
	// Copy the scope so for blocks can shadow variables without leaking to the caller.
	sc := make(expr.Scope, len(scope))
	for k, v := range scope {
		sc[k] = v
	}
	r := &renderer{ev: cfg.Evaluator, ph: cfg.Placeholder, lit: cfg.Literal, scope: sc}
	if err := r.visit(n); err != nil {
		return Result{}, err
	}
	return Result{SQL: r.sqlBuf.String(), Args: r.args, ArgSpans: r.spans}, nil
}

type renderer struct {
	ev  expr.Evaluator
	ph  dialect.Placeholder
	lit dialect.Literal

	scope  expr.Scope
	sqlBuf strings.Builder // executable placeholder form
	args   []any
	spans  [][2]int // byte range of each placeholder in sqlBuf, aligned with args
	nargs  int      // running bind count = placeholder index source
}

// emit writes literal text to the SQL buffer.
func (r *renderer) emit(s string) {
	r.sqlBuf.WriteString(s)
}

// bindOne emits one bound value: a placeholder into the SQL buffer and the value into args,
// recording the placeholder's byte range so the values-embedded form can be reconstructed
// later without re-rendering.
func (r *renderer) bindOne(v any) {
	r.nargs++
	start := r.sqlBuf.Len()
	r.sqlBuf.WriteString(r.ph(r.nargs, ""))
	r.spans = append(r.spans, [2]int{start, r.sqlBuf.Len()})
	r.args = append(r.args, v)
}

func (r *renderer) visit(n ast.Node) error {
	switch node := n.(type) {
	case ast.Statement:
		for _, c := range node.Nodes {
			if err := r.visit(c); err != nil {
				return err
			}
		}
		return nil
	case ast.Paren:
		r.emit("(")
		if err := r.visit(node.Node); err != nil {
			return err
		}
		r.emit(")")
		return nil
	case ast.Word:
		r.emit(node.Token)
		return nil
	case ast.Other:
		r.emit(node.Token)
		return nil
	case ast.Comment:
		r.emit(node.Token)
		return nil
	case ast.Space:
		r.emit(node.Token)
		return nil
	case ast.Eol:
		r.emit(node.Token)
		return nil
	case ast.BindValue:
		return r.visitBind(node)
	case ast.LiteralValue:
		return r.visitLiteral(node)
	case ast.IfBlock:
		return r.visitIf(node)
	case ast.ForBlock:
		return r.visitFor(node)
	default:
		return fmt.Errorf("bisql/render: unexpected node %T", n)
	}
}

func (r *renderer) eval(exprStr string) (any, error) {
	v, err := r.ev.Eval(exprStr, r.scope)
	if err != nil {
		return nil, fmt.Errorf("bisql/render: evaluating %q: %w", exprStr, err)
	}
	return v, nil
}

// visitBind expands into an IN list when the test literal is a parenthesized group, and
// otherwise binds the value as a single parameter (so a slice becomes one array parameter,
// e.g. Postgres `= ANY($1::type[])`). Whether the target dialect supports array binding is
// the driver's concern.
func (r *renderer) visitBind(node ast.BindValue) error {
	v, err := r.eval(node.Expression)
	if err != nil {
		return err
	}
	if _, isParen := node.Test.(ast.Paren); isParen {
		elems, ok := asIterable(v)
		if !ok {
			elems = []any{v} // scalar with a paren test -> single-element list
		}
		r.emit("(")
		if len(elems) == 0 {
			r.emit("null")
		}
		for i, e := range elems {
			if i > 0 {
				r.emit(", ")
			}
			if tup, ok := asIterable(e); ok { // a multi-column row, e.g. (a,b) IN ((1,2),(3,4))
				r.emit("(")
				for j, te := range tup {
					if j > 0 {
						r.emit(", ")
					}
					r.bindOne(te)
				}
				r.emit(")")
			} else {
				r.bindOne(e)
			}
		}
		r.emit(")")
	} else {
		r.bindOne(v)
	}
	for _, c := range node.Trailing {
		if err := r.visit(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) visitLiteral(node ast.LiteralValue) error {
	v, err := r.eval(node.Expression)
	if err != nil {
		return err
	}
	lit, err := r.lit(v)
	if err != nil {
		return fmt.Errorf("bisql/render: literal %q: %w", node.Expression, err)
	}
	r.emit(lit)
	for _, c := range node.Trailing {
		if err := r.visit(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) visitIf(node ast.IfBlock) error {
	chosen, err := r.chooseBranch(node)
	if err != nil {
		return err
	}
	for _, c := range chosen {
		if err := r.visit(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) chooseBranch(node ast.IfBlock) ([]ast.Node, error) {
	ok, err := r.evalBool(node.If.Expression)
	if err != nil {
		return nil, err
	}
	if ok {
		return node.If.Nodes, nil
	}
	for _, ei := range node.Elseif {
		ok, err := r.evalBool(ei.Expression)
		if err != nil {
			return nil, err
		}
		if ok {
			return ei.Nodes, nil
		}
	}
	if node.Else != nil {
		return node.Else.Nodes, nil
	}
	return nil, nil
}

func (r *renderer) evalBool(exprStr string) (bool, error) {
	v, err := r.eval(exprStr)
	if err != nil {
		return false, err
	}
	if v == nil {
		return false, nil // nil/absent condition is falsy (needs no guard)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("bisql/render: expression %q is not a boolean (got %T)", exprStr, v)
	}
	return b, nil
}

func (r *renderer) visitFor(node ast.ForBlock) error {
	v, err := r.eval(node.For.Expression)
	if err != nil {
		return err
	}
	if v == nil {
		return nil // nil/absent iterable = zero iterations
	}
	elems, ok := asIterable(v)
	if !ok {
		return fmt.Errorf("bisql/render: for expression %q is not iterable (got %T)", node.For.Expression, v)
	}
	id := node.For.Identifier
	// The loop variable and the helper variables _index / _has_next shadow the scope for
	// the loop's duration; save any pre-existing values and restore them afterwards. Only
	// _index and _has_next are exposed (usable inside /*%if*/); there is no _next_comma etc.
	// since raw-text emission (/*# */) was removed — build separated lists with an anchor
	// plus /*%if x_has_next*/,/*%end*/.
	names := []string{id, id + "_index", id + "_has_next"}
	saved := make(map[string]any, len(names))
	had := make(map[string]bool, len(names))
	for _, n := range names {
		if val, ok := r.scope[n]; ok {
			saved[n] = val
			had[n] = true
		}
	}
	for i, e := range elems {
		r.scope[id] = e
		r.scope[id+"_index"] = i
		r.scope[id+"_has_next"] = i < len(elems)-1
		for _, c := range node.For.Nodes {
			if err := r.visit(c); err != nil {
				return err
			}
		}
	}
	for _, n := range names {
		if had[n] {
			r.scope[n] = saved[n]
		} else {
			delete(r.scope, n)
		}
	}
	return nil
}
