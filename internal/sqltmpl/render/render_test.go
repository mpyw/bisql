package render

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// stubEval isolates render from the real expression language: an expression is looked up
// as a scope key first, then in the evaluator's own overrides; unknown names error.
type stubEval map[string]any

func (s stubEval) Eval(expression string, scope expr.Scope) (any, error) {
	if v, ok := scope[expression]; ok {
		return v, nil
	}
	if v, ok := s[expression]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("stub: unknown %q", expression)
}

// qmark is a simple placeholder generator (MySQL-style).
func qmark(int, string) string { return "?" }

// litFn is a minimal literal formatter for tests.
func litFn(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	return fmt.Sprintf("%v", v), nil
}

func newCfg(ev expr.Evaluator) Config {
	return Config{Evaluator: ev, Placeholder: qmark, Literal: litFn}
}

// --- blank normalization (flushBlanks: keep-from-last-EOL) ---

func TestRenderBlankNormalization(t *testing.T) {
	t.Run("spaces only are preserved verbatim", func(t *testing.T) {
		tree := ast.Statement{Nodes: []ast.Node{
			ast.Word{Token: "a"},
			ast.Space{Token: " "}, ast.Space{Token: " "},
			ast.Word{Token: "b"},
		}}
		res, err := Render(tree, nil, newCfg(stubEval{}))
		if err != nil {
			t.Fatal(err)
		}
		if res.SQL != "a  b" {
			t.Errorf("got %q, want %q", res.SQL, "a  b")
		}
	})

	t.Run("blanks before the last EOL collapse", func(t *testing.T) {
		// Between a and b: EOL, space, EOL, "  " -> keep from the last EOL: "\n  ".
		tree := ast.Statement{Nodes: []ast.Node{
			ast.Word{Token: "a"},
			ast.Eol{Token: "\n"}, ast.Space{Token: " "},
			ast.Eol{Token: "\n"}, ast.Space{Token: "  "},
			ast.Word{Token: "b"},
		}}
		res, err := Render(tree, nil, newCfg(stubEval{}))
		if err != nil {
			t.Fatal(err)
		}
		if res.SQL != "a\n  b" {
			t.Errorf("got %q, want %q", res.SQL, "a\n  b")
		}
	})
}

// --- Config seams: custom placeholder and literal, produced in one pass ---

func TestRenderPlaceholderAndLiteral(t *testing.T) {
	// where a = /*v*/  with v = 7
	tree := ast.Statement{Nodes: []ast.Node{
		ast.Word{Token: "a"}, ast.Space{Token: " "}, ast.Other{Token: "="}, ast.Space{Token: " "},
		ast.BindValue{Expression: "v", Test: ast.Word{Token: "0"}},
	}}
	cfg := Config{
		Evaluator:   stubEval{"v": 7},
		Placeholder: func(i int, _ string) string { return fmt.Sprintf("$%d", i) },
		Literal:     litFn,
	}
	res, err := Render(tree, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "a = $1" {
		t.Errorf("SQL got %q", res.SQL)
	}
	if !reflect.DeepEqual(res.Args, []any{7}) {
		t.Errorf("Args got %#v", res.Args)
	}
	if res.SQLWithArgs != "a = 7" {
		t.Errorf("SQLWithArgs got %q", res.SQLWithArgs)
	}
}

// --- bind IN expansion at the render level ---

func TestRenderINExpansion(t *testing.T) {
	mk := func() ast.Statement {
		return ast.Statement{Nodes: []ast.Node{
			ast.BindValue{Expression: "ids", Test: ast.Word{Token: "0"}},
		}}
	}
	t.Run("list", func(t *testing.T) {
		res, err := Render(mk(), nil, newCfg(stubEval{"ids": []any{1, 2, 3}}))
		if err != nil {
			t.Fatal(err)
		}
		if res.SQL != "(?, ?, ?)" || !reflect.DeepEqual(res.Args, []any{1, 2, 3}) {
			t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
		}
		if res.SQLWithArgs != "(1, 2, 3)" {
			t.Errorf("SQLWithArgs=%q", res.SQLWithArgs)
		}
	})
	t.Run("empty list -> (null)", func(t *testing.T) {
		res, err := Render(mk(), nil, newCfg(stubEval{"ids": []any{}}))
		if err != nil {
			t.Fatal(err)
		}
		if res.SQL != "(null)" || len(res.Args) != 0 {
			t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
		}
	})
}

// --- for-loop helper variables + scope restoration ---

// capEval returns the iterable for the loop expression and snapshots the loop-scoped
// variables each time the body's marker expression is evaluated.
type capEval struct {
	xs    []any
	id    string
	keys  []string
	snaps []map[string]any
	after map[string]any
}

func (c *capEval) Eval(e string, scope expr.Scope) (any, error) {
	switch e {
	case "xs":
		return c.xs, nil
	case "__snap__":
		m := map[string]any{}
		for _, k := range c.keys {
			m[k] = scope[k]
		}
		c.snaps = append(c.snaps, m)
		return "", nil // embedded empty -> renders nothing
	case "__after__":
		c.after = map[string]any{c.id: scope[c.id], c.id + "_index": scope[c.id+"_index"]}
		return "", nil
	default:
		return nil, fmt.Errorf("unknown %q", e)
	}
}

func TestRenderForHelpers(t *testing.T) {
	id := "i"
	ev := &capEval{
		xs:   []any{10, 20, 30},
		id:   id,
		keys: []string{id, id + "_index", id + "_has_next", id + "_next_comma", id + "_next_and", id + "_next_or"},
	}
	// for i in xs { <snapshot> } <after-snapshot>
	forBlock := ast.ForBlock{For: ast.ForDirective{
		Identifier: id,
		Expression: "xs",
		Nodes:      []ast.Node{ast.EmbeddedValue{Expression: "__snap__"}},
	}}
	tree := ast.Statement{Nodes: []ast.Node{forBlock, ast.EmbeddedValue{Expression: "__after__"}}}

	// Seed the scope with a pre-existing "i" to prove it is restored after the loop.
	if _, err := Render(tree, expr.Scope{id: "OUTER"}, newCfg(ev)); err != nil {
		t.Fatal(err)
	}

	want := []map[string]any{
		{"i": 10, "i_index": 0, "i_has_next": true, "i_next_comma": ",", "i_next_and": "and", "i_next_or": "or"},
		{"i": 20, "i_index": 1, "i_has_next": true, "i_next_comma": ",", "i_next_and": "and", "i_next_or": "or"},
		{"i": 30, "i_index": 2, "i_has_next": false, "i_next_comma": "", "i_next_and": "", "i_next_or": ""},
	}
	if !reflect.DeepEqual(ev.snaps, want) {
		t.Errorf("per-iteration helpers\n got: %#v\nwant: %#v", ev.snaps, want)
	}

	// After the loop: "i" restored to its pre-loop value, helper vars removed.
	if ev.after["i"] != "OUTER" {
		t.Errorf("i after loop = %#v, want restored to \"OUTER\"", ev.after["i"])
	}
	if ev.after["i_index"] != nil {
		t.Errorf("i_index after loop = %#v, want nil (removed)", ev.after["i_index"])
	}
}

// --- recursion depth guard ---

func TestRenderDepthGuard(t *testing.T) {
	// The embedded value re-parses to a reference to itself, so it never terminates.
	tree := ast.Statement{Nodes: []ast.Node{ast.EmbeddedValue{Expression: "self"}}}
	cfg := newCfg(stubEval{"self": "/*# self */"})
	cfg.MaxDepth = 4
	_, err := Render(tree, nil, cfg)
	if err == nil {
		t.Fatal("expected a depth-exceeded error")
	}
}

// --- partial resolution at the render level ---

func TestRenderPartial(t *testing.T) {
	partial := ast.Statement{Nodes: []ast.Node{ast.Word{Token: "x"}, ast.Space{Token: " "}, ast.Other{Token: "="}, ast.Space{Token: " "}, ast.Word{Token: "1"}}}
	tree := ast.Statement{Nodes: []ast.Node{ast.Partial{Name: "p"}}}

	t.Run("resolved and spliced", func(t *testing.T) {
		cfg := newCfg(stubEval{})
		cfg.Resolve = func(name string) (ast.Node, error) {
			if name != "p" {
				return nil, fmt.Errorf("unknown %q", name)
			}
			return partial, nil
		}
		res, err := Render(tree, nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if res.SQL != "x = 1" {
			t.Errorf("got %q", res.SQL)
		}
	})

	t.Run("no resolver -> error", func(t *testing.T) {
		if _, err := Render(tree, nil, newCfg(stubEval{})); err == nil {
			t.Fatal("expected error when Resolve is nil")
		}
	})

	t.Run("cycle detected", func(t *testing.T) {
		cfg := newCfg(stubEval{})
		// p resolves to a tree that references p again.
		cfg.Resolve = func(string) (ast.Node, error) {
			return ast.Statement{Nodes: []ast.Node{ast.Partial{Name: "p"}}}, nil
		}
		if _, err := Render(tree, nil, cfg); err == nil {
			t.Fatal("expected a cyclic-reference error")
		}
	})
}

// --- empty droppable clause is removed; Select/From always kept ---

func TestRenderClauseRemoval(t *testing.T) {
	// select <from-clause>, where the WHERE body is empty -> WHERE dropped, FROM kept.
	where := ast.Clause{Kind: ast.ClauseWhere, Keyword: "where", Nodes: []ast.Node{ast.Space{Token: " "}}}
	from := ast.Clause{Kind: ast.ClauseFrom, Keyword: "from", Nodes: []ast.Node{ast.Space{Token: " "}, ast.Word{Token: "t"}, ast.Space{Token: " "}, where}}
	sel := ast.Clause{Kind: ast.ClauseSelect, Keyword: "select", Nodes: []ast.Node{ast.Space{Token: " "}, ast.Word{Token: "1"}, ast.Space{Token: " "}, from}}
	tree := ast.Statement{Nodes: []ast.Node{sel}}

	res, err := Render(tree, nil, newCfg(stubEval{}))
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "select 1 from t " {
		t.Errorf("got %q, want %q", res.SQL, "select 1 from t ")
	}
}
