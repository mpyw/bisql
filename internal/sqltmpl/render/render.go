// Package render evaluates the template tree into (SQL, args). This is the heart of
// 2-way SQL: it propagates an "available" flag to drop clauses that become empty and
// leading AND/OR left dangling. Ported from Komapper's TwoWayTemplateStatementBuilder
// (see docs/komapper-analysis.md).
package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
	"github.com/mpyw/bisql/pkg/dialect"
	"github.com/mpyw/bisql/pkg/expr"
)

// Result is the rendered statement.
type Result struct {
	SQL  string
	Args []any
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
	// EmbedValues renders bound values inline as SQL literals (via Literal) instead of
	// placeholders, producing the values-embedded form for snapshots/review. Result.Args is
	// then empty. Never execute this form.
	EmbedValues bool
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
		embed:    cfg.EmbedValues,
		active:   map[string]bool{},
	}
	st := newState(scope)
	if err := r.visit(st, n); err != nil {
		return Result{}, err
	}
	st.flushBlanks()
	return Result{SQL: st.buf.String(), Args: st.args}, nil
}

type renderer struct {
	ev       expr.Evaluator
	ph       dialect.Placeholder
	lit      dialect.Literal
	resolve  func(name string) (ast.Node, error)
	maxDepth int
	embed    bool

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

// state accumulates rendered SQL and args for one scope. available reports whether real
// SQL body (a Word or Other token, a Paren, or a non-empty embedded value) has been
// emitted; blank nodes are buffered until the next real output so they can be dropped or
// normalized (see flushBlanks).
type state struct {
	buf       strings.Builder
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
	s.buf.WriteString(str)
}

func (s *state) appendBlank(tok string) { s.blanks = append(s.blanks, tok) }

// appendState flushes both buffers and merges the child's buffer and args into s.
func (s *state) appendState(c *state) {
	s.flushBlanks()
	c.flushBlanks()
	s.buf.WriteString(c.buf.String())
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
		s.buf.WriteString(b)
	}
	s.blanks = nil
}

func isEol(tok string) bool { return tok == "\n" || tok == "\r\n" || tok == "\r" }

// clauseRe matches a buffer that begins with a clause keyword (after trimming). Used to
// keep a clause whose only content is a nested clause (e.g. WHERE holding an ORDER BY).
var clauseRe = regexp.MustCompile(`(?i)^(select|from|where|group by|having|order by|for update|option)\s`)

func (s *state) startsWithClause() bool {
	s.flushBlanks()
	return clauseRe.MatchString(strings.TrimSpace(s.buf.String()))
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
		s.available = true
		s.appendString("(")
		if err := r.visit(s, node.Node); err != nil {
			return err
		}
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

// bindOne emits a single bound value: a placeholder (collecting the value into args) in the
// normal mode, or an inline SQL literal in the values-embedded mode.
func (r *renderer) bindOne(s *state, v any) error {
	if r.embed {
		lit, err := r.lit(v)
		if err != nil {
			return fmt.Errorf("bisql/render: embedding value: %w", err)
		}
		s.appendString(lit)
		return nil
	}
	s.flushBlanks()
	s.buf.WriteString(r.ph(len(s.args)+1, ""))
	s.args = append(s.args, v)
	return nil
}

func (r *renderer) visitBind(s *state, node ast.BindValue) error {
	v, err := r.eval(node.Expression, s)
	if err != nil {
		return err
	}
	if elems, ok := asIterable(v); ok {
		s.appendString("(")
		if len(elems) == 0 {
			s.appendString("null")
		}
		for i, e := range elems {
			if i > 0 {
				s.appendString(", ")
			}
			if tup, ok := asTuple(e); ok {
				s.appendString("(")
				for j, te := range tup {
					if j > 0 {
						s.appendString(", ")
					}
					if err := r.bindOne(s, te); err != nil {
						return err
					}
				}
				s.appendString(")")
			} else {
				if err := r.bindOne(s, e); err != nil {
					return err
				}
			}
		}
		s.appendString(")")
	} else if err := r.bindOne(s, v); err != nil {
		return err
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
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("bisql/render: expression %q is not a boolean (got %T)", exprStr, v)
	}
	return b, nil
}

func (r *renderer) visitFor(s *state, node ast.ForBlock) error {
	v, err := r.eval(node.For.Expression, s)
	if err != nil {
		return err
	}
	elems, ok := asIterable(v)
	if !ok {
		return fmt.Errorf("bisql/render: for expression %q is not iterable (got %T)", node.For.Expression, v)
	}
	id := node.For.Identifier
	saved, hadSaved := s.scope[id]
	helper := []string{id + "_index", id + "_has_next", id + "_next_comma", id + "_next_and", id + "_next_or"}
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
	if hadSaved {
		s.scope[id] = saved
	} else {
		delete(s.scope, id)
	}
	for _, h := range helper {
		delete(s.scope, h)
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
