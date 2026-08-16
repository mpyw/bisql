// Package render evaluates the template tree into (SQL, args). This is the heart of
// 2-way SQL: it propagates an "available" flag to drop clauses that become empty and
// leading AND/OR left dangling. Ported from Komapper's TwoWayTemplateStatementBuilder
// (see docs/komapper-analysis.md).
package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mpyw/bisql/dialect"
	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
)

// Result is the rendered statement. SQL/Args are the executable placeholder form;
// SQLWithArgs is the values-embedded form for snapshots/review (never execute it). All
// three are produced in a single pass.
type Result struct {
	SQL         string
	Args        []any
	SQLWithArgs string
}

// DefaultMaxDepth bounds recursive expansion of embedded values and partials, guarding
// against runaway (e.g. an embedded value that reproduces itself) even when no name cycle
// is present.
const DefaultMaxDepth = 50

// Config carries everything the renderer needs beyond the tree and the scope.
type Config struct {
	Evaluator   expr.Evaluator
	Placeholder dialect.Placeholder
	Literal     dialect.Literal
	// Resolve returns the parsed tree for a partial name (/*> name */). Nil means partials
	// are unsupported; encountering one is then an error.
	Resolve func(name string) (ast.Node, error)
	// MaxDepth bounds recursive expansion; <= 0 uses DefaultMaxDepth.
	MaxDepth int
}

// Render evaluates the tree against the scope, producing the placeholder-form SQL and its
// bind arguments.
func Render(n ast.Node, scope expr.Scope, cfg Config) (Result, error) {
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	r := &renderer{
		ev:       cfg.Evaluator,
		ph:       cfg.Placeholder,
		lit:      cfg.Literal,
		resolve:  cfg.Resolve,
		maxDepth: maxDepth,
		active:   map[string]bool{},
	}
	st := newState(scope)
	if err := r.visit(st, n); err != nil {
		return Result{}, err
	}
	st.flushBlanks()
	return Result{SQL: st.sql.String(), Args: st.args, SQLWithArgs: st.lit.String()}, nil
}

type renderer struct {
	ev       expr.Evaluator
	ph       dialect.Placeholder
	lit      dialect.Literal
	resolve  func(name string) (ast.Node, error)
	maxDepth int

	// nargs is the running count of emitted binds; it is the source of the placeholder
	// index. It must be renderer-global (not per-state): child states hold their own args
	// slices that are merged later, so deriving the index from a per-state length would
	// restart numbering inside every droppable clause / set operand / spliced partial and
	// break every index-based dialect ($n, :n, @pn). Because a bind sets available (so a
	// bind-bearing subtree is never dropped), every counted bind survives into the final
	// args in visitation order — keeping the placeholder number equal to the arg position.
	nargs int

	// recursion tracking for embedded values and partials
	depth  int
	active map[string]bool // partial names currently being expanded (cycle detection)
}

// expand renders a re-parsed sub-tree (from an embedded value or a partial) inline into the
// current state, bounded by maxDepth. Shared by embedded values and partials: both splice
// their content into the surrounding SQL so directives/binds within them are evaluated.
func (r *renderer) expand(s *state, sub ast.Node) error {
	if r.depth >= r.maxDepth {
		return fmt.Errorf("bisql/render: expansion depth exceeded %d (possible recursive embed or partial cycle)", r.maxDepth)
	}
	r.depth++
	defer func() { r.depth-- }()
	return r.visit(s, sub)
}

// state accumulates rendered SQL for one scope. Two buffers are built in parallel: sql
// holds the executable placeholder form, lit the values-embedded form; they diverge only
// at bound values. available reports whether real SQL body (a Word or Other token, a
// Paren, or a non-empty embedded value) has been emitted; blank nodes are buffered until
// the next real output so they can be dropped or normalized (see flushBlanks).
type state struct {
	sql       strings.Builder
	lit       strings.Builder
	args      []any
	scope     expr.Scope
	available bool
	blanks    []string
}

func newState(scope expr.Scope) *state {
	// Copy the scope so for/with blocks can shadow variables without leaking to siblings.
	cp := make(expr.Scope, len(scope))
	for k, v := range scope {
		cp[k] = v
	}
	return &state{scope: cp}
}

func (s *state) child() *state { return newState(s.scope) }

func (s *state) appendString(str string) {
	s.flushBlanks()
	s.sql.WriteString(str)
	s.lit.WriteString(str)
}

func (s *state) appendBlank(tok string) { s.blanks = append(s.blanks, tok) }

// appendState flushes both states' blanks and merges the child's buffers and args into s.
func (s *state) appendState(c *state) {
	s.flushBlanks()
	c.flushBlanks()
	s.sql.WriteString(c.sql.String())
	s.lit.WriteString(c.lit.String())
	s.args = append(s.args, c.args...)
}

// flushBlanks emits buffered blank tokens. When any EOL is present, everything up to and
// including the last EOL is dropped (whitespace normalization, matching Komapper): the
// result keeps the final newline and whatever trailing spaces followed it.
func (s *state) flushBlanks() {
	if len(s.blanks) == 0 {
		return
	}
	blanks := s.blanks
	lastEol := -1
	for i, b := range blanks {
		if isEol(b) {
			lastEol = i
		}
	}
	if lastEol >= 0 {
		blanks = blanks[lastEol:]
	}
	for _, b := range blanks {
		s.sql.WriteString(b)
		s.lit.WriteString(b)
	}
	s.blanks = nil
}

func isEol(tok string) bool { return tok == "\n" || tok == "\r\n" || tok == "\r" }

// clauseRe matches a buffer that begins with a clause keyword (after trimming). Used to
// keep a clause whose only content is a nested clause (e.g. WHERE holding an ORDER BY).
var clauseRe = regexp.MustCompile(`(?i)^(select|from|where|group by|having|order by|for update|option)\s`)

func (s *state) startsWithClause() bool {
	s.flushBlanks()
	// The two buffers differ only at bound values, which never begin a clause, so either
	// works for clause-keyword detection.
	return clauseRe.MatchString(strings.TrimSpace(s.sql.String()))
}

func (r *renderer) visit(s *state, n ast.Node) error {
	switch node := n.(type) {
	case ast.Statement:
		for _, c := range node.Nodes {
			if err := r.visit(s, c); err != nil {
				return err
			}
		}
		return nil

	case ast.Set:
		left := s.child()
		if err := r.visit(left, node.Left); err != nil {
			return err
		}
		if left.available {
			s.available = true
			s.appendState(left)
		}
		right := s.child()
		if err := r.visit(right, node.Right); err != nil {
			return err
		}
		if right.available {
			if left.available {
				s.appendString(node.Keyword)
			}
			s.available = true
			s.appendState(right)
		}
		return nil

	case ast.Paren:
		// The parens are content for the enclosing clause (mark available), but inside them
		// availability must reset so a dangling leading AND/OR is dropped. Rendering into a
		// child state achieves that; Komapper renders into the same state and thus leaves
		// "(and ...)" — bisql diverges here for correctness.
		s.available = true
		s.appendString("(")
		child := s.child()
		if err := r.visit(child, node.Node); err != nil {
			return err
		}
		s.appendState(child)
		s.appendString(")")
		return nil

	case ast.Clause:
		return r.visitClause(s, node)

	case ast.Logical:
		// Emit the AND/OR keyword only when preceding content exists; otherwise it is a
		// dangling operator and is dropped, but its body still renders.
		if s.available {
			s.appendString(node.Keyword)
		}
		for _, c := range node.Nodes {
			if err := r.visit(s, c); err != nil {
				return err
			}
		}
		return nil

	case ast.Word:
		s.available = true
		s.appendString(node.Token)
		return nil
	case ast.Other:
		s.available = true
		s.appendString(node.Token)
		return nil
	case ast.Comment:
		s.appendString(node.Token)
		return nil
	case ast.Space:
		s.appendBlank(node.Token)
		return nil
	case ast.Eol:
		s.appendBlank(node.Token)
		return nil

	case ast.BindValue:
		return r.visitBind(s, node)
	case ast.LiteralValue:
		return r.visitLiteral(s, node)
	case ast.EmbeddedValue:
		return r.visitEmbedded(s, node)
	case ast.Partial:
		return r.visitPartial(s, node)

	case ast.IfBlock:
		return r.visitIf(s, node)
	case ast.ForBlock:
		return r.visitFor(s, node)
	case ast.WithBlock:
		return r.visitWith(s, node)

	default:
		return fmt.Errorf("bisql/render: unexpected node %T", n)
	}
}

func (r *renderer) visitClause(s *state, node ast.Clause) error {
	// Select/From/ForUpdate are always emitted; other clauses drop when their body is empty.
	switch node.Kind {
	case ast.ClauseSelect, ast.ClauseFrom, ast.ClauseForUpdate:
		s.appendString(node.Keyword)
		for _, c := range node.Nodes {
			if err := r.visit(s, c); err != nil {
				return err
			}
		}
		return nil
	}
	child := s.child()
	for _, c := range node.Nodes {
		if err := r.visit(child, c); err != nil {
			return err
		}
	}
	if child.available {
		s.appendString(node.Keyword)
		s.appendState(child)
	} else if child.startsWithClause() {
		// The body carries a nested clause (e.g. ORDER BY inside WHERE); keep it, dropping
		// only this clause's own keyword.
		s.available = true
		s.appendState(child)
	}
	return nil
}

func (r *renderer) eval(exprStr string, s *state) (any, error) {
	v, err := r.ev.Eval(exprStr, s.scope)
	if err != nil {
		return nil, fmt.Errorf("bisql/render: evaluating %q: %w", exprStr, err)
	}
	return v, nil
}

// bindOne emits one bound value into both buffers: a placeholder into the executable sql
// buffer (collecting the value into args), and an inline SQL literal into the lit buffer.
// A literal-formatting failure only affects the review form, so it falls back to %v rather
// than failing the render.
func (r *renderer) bindOne(s *state, v any) {
	s.flushBlanks()
	r.nargs++
	s.sql.WriteString(r.ph(r.nargs, ""))
	s.args = append(s.args, v)
	litStr, err := r.lit(v)
	if err != nil {
		litStr = fmt.Sprintf("%v", v)
	}
	s.lit.WriteString(litStr)
}

func (r *renderer) visitBind(s *state, node ast.BindValue) error {
	v, err := r.eval(node.Expression, s)
	if err != nil {
		return err
	}
	// A bind is real content: mark the state available so a clause or set operand whose
	// only content is a bind is kept (not silently dropped, discarding the value). This
	// also guarantees no counted bind lives in a dropped subtree (see renderer.nargs).
	s.available = true
	if elems, ok := asIterable(v); ok {
		s.appendString("(")
		if len(elems) == 0 {
			s.appendString("null")
		}
		for i, e := range elems {
			if i > 0 {
				s.appendString(", ")
			}
			// An element that is itself a slice/array is a multi-column row (tuple), e.g.
			// (a, b) IN ((1, 2), (3, 4)); any arity is supported.
			if tup, ok := asIterable(e); ok {
				s.appendString("(")
				for j, te := range tup {
					if j > 0 {
						s.appendString(", ")
					}
					r.bindOne(s, te)
				}
				s.appendString(")")
			} else {
				r.bindOne(s, e)
			}
		}
		s.appendString(")")
	} else {
		r.bindOne(s, v)
	}
	// Trailing nodes (whatever followed the test literal) render after the placeholder.
	for _, c := range node.Trailing {
		if err := r.visit(s, c); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) visitLiteral(s *state, node ast.LiteralValue) error {
	v, err := r.eval(node.Expression, s)
	if err != nil {
		return err
	}
	lit, err := r.lit(v)
	if err != nil {
		return fmt.Errorf("bisql/render: literal %q: %w", node.Expression, err)
	}
	// A literal is real content (like a bind); keep its enclosing clause/operand.
	s.available = true
	s.appendString(lit)
	for _, c := range node.Trailing {
		if err := r.visit(s, c); err != nil {
			return err
		}
	}
	return nil
}

// visitEmbedded expands /*# expr */. The expression is evaluated against the scope to a
// string, which is then re-parsed and spliced in recursively — so directives, binds, and
// nested embeds/partials inside the produced text are evaluated.
//
// Note: because the text comes from a runtime value, only trusted, developer-controlled
// fragments (column names, precomputed clauses) should flow through an embedded value;
// re-parsing attacker-controlled data here is an injection surface.
func (r *renderer) visitEmbedded(s *state, node ast.EmbeddedValue) error {
	v, err := r.eval(node.Expression, s)
	if err != nil {
		return err
	}
	str := toStr(v)
	if str == "" {
		return nil
	}
	sub, err := parser.Parse(str)
	if err != nil {
		return fmt.Errorf("bisql/render: parsing embedded value %q: %w", node.Expression, err)
	}
	return r.expand(s, sub)
}

// visitPartial expands /*> name */. The named fragment is resolved to its parsed tree and
// spliced in recursively (so a partial may reference further partials). The distinction
// from an embedded value is only the source: a static, loader-registered fragment vs. a
// runtime scope expression. Cyclic references are detected by name.
func (r *renderer) visitPartial(s *state, node ast.Partial) error {
	if r.resolve == nil {
		return fmt.Errorf("bisql/render: partial %q requires a Loader (use bisql.NewLoader)", node.Name)
	}
	if r.active[node.Name] {
		return fmt.Errorf("bisql/render: cyclic partial reference %q", node.Name)
	}
	sub, err := r.resolve(node.Name)
	if err != nil {
		return err
	}
	r.active[node.Name] = true
	defer delete(r.active, node.Name)
	return r.expand(s, sub)
}

func (r *renderer) visitIf(s *state, node ast.IfBlock) error {
	chosen, err := r.chooseBranch(s, node)
	if err != nil {
		return err
	}
	for _, c := range chosen {
		if err := r.visit(s, c); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) chooseBranch(s *state, node ast.IfBlock) ([]ast.Node, error) {
	ok, err := r.evalBool(node.If.Expression, s)
	if err != nil {
		return nil, err
	}
	if ok {
		return node.If.Nodes, nil
	}
	for _, ei := range node.Elseif {
		ok, err := r.evalBool(ei.Expression, s)
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

func (r *renderer) evalBool(exprStr string, s *state) (bool, error) {
	v, err := r.eval(exprStr, s)
	if err != nil {
		return false, err
	}
	if v == nil {
		// A nil (or absent, under AllowUndefinedVariables) condition is falsy, so
		// /*%if activeOnly*/ needs no defensive guard — mirroring the for-loop's nil rule.
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		// A non-nil, non-boolean condition is a type error (e.g. /*%if name*/ where name is
		// a string — the author likely meant /*%if name != null*/).
		return false, fmt.Errorf("bisql/render: expression %q is not a boolean (got %T)", exprStr, v)
	}
	return b, nil
}

func (r *renderer) visitFor(s *state, node ast.ForBlock) error {
	v, err := r.eval(node.For.Expression, s)
	if err != nil {
		return err
	}
	if v == nil {
		// A nil (or absent, under AllowUndefinedVariables) iterable means "no items": zero
		// iterations, so the surrounding droppable clause drops natively. This spares the
		// caller a /*%if xs != null*/ guard around every loop. A non-nil, non-iterable
		// value is still a type error below.
		return nil
	}
	elems, ok := asIterable(v)
	if !ok {
		return fmt.Errorf("bisql/render: for expression %q is not iterable (got %T)", node.For.Expression, v)
	}
	id := node.For.Identifier
	// The loop variable and its helper variables shadow the scope for the loop's duration;
	// save any pre-existing values (a caller could have a key named like a helper, or an
	// outer loop with the same identifier) and restore them afterwards.
	names := []string{id, id + "_index", id + "_has_next", id + "_next_comma", id + "_next_and", id + "_next_or"}
	saved := make(map[string]any, len(names))
	had := make(map[string]bool, len(names))
	for _, n := range names {
		if v, ok := s.scope[n]; ok {
			saved[n] = v
			had[n] = true
		}
	}
	for i, e := range elems {
		hasNext := i < len(elems)-1
		s.scope[id] = e
		s.scope[id+"_index"] = i
		s.scope[id+"_has_next"] = hasNext
		s.scope[id+"_next_comma"] = sep(hasNext, ",")
		s.scope[id+"_next_and"] = sep(hasNext, "and")
		s.scope[id+"_next_or"] = sep(hasNext, "or")
		for _, c := range node.For.Nodes {
			if err := r.visit(s, c); err != nil {
				return err
			}
		}
	}
	for _, n := range names {
		if had[n] {
			s.scope[n] = saved[n]
		} else {
			delete(s.scope, n)
		}
	}
	return nil
}

func (r *renderer) visitWith(s *state, node ast.WithBlock) error {
	v, err := r.eval(node.With.Expression, s)
	if err != nil {
		return err
	}
	if v == nil {
		return fmt.Errorf("bisql/render: with expression %q is nil", node.With.Expression)
	}
	members, err := membersOf(v)
	if err != nil {
		return fmt.Errorf("bisql/render: with expression %q: %w", node.With.Expression, err)
	}
	// Snapshot the whole scope and restore afterwards (matching Komapper).
	preserved := make(expr.Scope, len(s.scope))
	for k, val := range s.scope {
		preserved[k] = val
	}
	for k, mv := range members {
		s.scope[k] = mv
	}
	for _, c := range node.With.Nodes {
		if err := r.visit(s, c); err != nil {
			return err
		}
	}
	for k := range s.scope {
		delete(s.scope, k)
	}
	for k, val := range preserved {
		s.scope[k] = val
	}
	return nil
}

func sep(hasNext bool, s string) string {
	if hasNext {
		return s
	}
	return ""
}
