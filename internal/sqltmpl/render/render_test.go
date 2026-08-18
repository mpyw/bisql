package render_test

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
	"github.com/mpyw/bisql/internal/sqltmpl/render"
)

// stubEval isolates render from the real expression language: an expression is a scope key.
type stubEval map[string]any

func (s stubEval) Eval(expression string, scope expr.Scope) (any, error) {
	if v, ok := scope[expression]; ok {
		return v, nil
	}
	if v, ok := s[expression]; ok {
		return v, nil
	}
	return nil, nil // undefined -> nil (matches AllowUndefinedVariables)
}

func qmark(int, string) string      { return "?" }
func dollar(n int, _ string) string { return "$" + strconv.Itoa(n) }

func litFn(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	return fmt.Sprintf("%v", v), nil
}

func renderTmpl(t *testing.T, tmpl string, scope expr.Scope, ph func(int, string) string) render.Result {
	t.Helper()
	n, err := parser.Parse(tmpl)
	if err != nil {
		t.Fatalf("parse %q: %v", tmpl, err)
	}
	res, err := render.Render(n, scope, render.Config{Evaluator: stubEval{}, Placeholder: ph, Literal: litFn})
	if err != nil {
		t.Fatalf("render %q: %v", tmpl, err)
	}
	return res
}

func TestRender_Verbatim(t *testing.T) {
	res := renderTmpl(t, "select *\nfrom t where 1 = 1", nil, qmark)
	if res.SQL != "select *\nfrom t where 1 = 1" {
		t.Errorf("got %q", res.SQL)
	}
}

func TestRender_Bind(t *testing.T) {
	res := renderTmpl(t, "a = /*x*/0", expr.Scope{"x": 5}, qmark)
	// The placeholder occupies bytes [4,5) of "a = ?"; the span lets a caller splice the
	// literal back in on demand (that is what bisql.Statement.SQLWithArgs does).
	if res.SQL != "a = ?" || !reflect.DeepEqual(res.Args, []any{5}) ||
		!reflect.DeepEqual(res.ArgSpans, [][2]int{{4, 5}}) {
		t.Errorf("SQL=%q Args=%#v Spans=%#v", res.SQL, res.Args, res.ArgSpans)
	}
}

func TestRender_INExpansion(t *testing.T) {
	// paren test -> expand
	res := renderTmpl(t, "id in /*ids*/(0)", expr.Scope{"ids": []any{1, 2, 3}}, qmark)
	if res.SQL != "id in (?, ?, ?)" || !reflect.DeepEqual(res.Args, []any{1, 2, 3}) {
		t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
	}
	// empty slice -> (null)
	res = renderTmpl(t, "id in /*ids*/(0)", expr.Scope{"ids": []any{}}, qmark)
	if res.SQL != "id in (null)" || len(res.Args) != 0 {
		t.Errorf("empty: SQL=%q Args=%#v", res.SQL, res.Args)
	}
	// tuple
	res = renderTmpl(t, "(a,b) in /*p*/((0,0))", expr.Scope{"p": []any{[]any{1, 2}, []any{3, 4}}}, qmark)
	if res.SQL != "(a,b) in ((?, ?), (?, ?))" || !reflect.DeepEqual(res.Args, []any{1, 2, 3, 4}) {
		t.Errorf("tuple: SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

// A scalar test literal binds the value as-is: a slice becomes ONE parameter (= ANY array).
func TestRender_ScalarBindOfSlice(t *testing.T) {
	res := renderTmpl(t, "ts = ANY(/*ts*/'{}')", expr.Scope{"ts": []any{"a", "b"}}, dollar)
	if res.SQL != "ts = ANY($1)" {
		t.Errorf("SQL=%q", res.SQL)
	}
	if len(res.Args) != 1 {
		t.Fatalf("want 1 arg (the array), got %#v", res.Args)
	}
	if !reflect.DeepEqual(res.Args[0], []any{"a", "b"}) {
		t.Errorf("arg = %#v", res.Args[0])
	}
}

// Placeholder numbering is a single global counter across the whole render.
func TestRender_PlaceholderNumbering(t *testing.T) {
	res := renderTmpl(t, "select /*a*/0 from t where b = /*b*/0 and c in /*cs*/(0)",
		expr.Scope{"a": 1, "b": 2, "cs": []any{3, 4}}, dollar)
	want := "select $1 from t where b = $2 and c in ($3, $4)"
	if res.SQL != want {
		t.Errorf("SQL\n got: %q\nwant: %q", res.SQL, want)
	}
	if !reflect.DeepEqual(res.Args, []any{1, 2, 3, 4}) {
		t.Errorf("Args %#v", res.Args)
	}
}

func TestRender_Literal(t *testing.T) {
	res := renderTmpl(t, "a = /*^v*/'x'", expr.Scope{"v": 42}, qmark)
	if res.SQL != "a = 42" || len(res.Args) != 0 {
		t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

func TestRender_If(t *testing.T) {
	tmpl := "select 1 from t /*%if flag*/where x = 1/*%end*/"
	if res := renderTmpl(t, tmpl, expr.Scope{"flag": true}, qmark); res.SQL != "select 1 from t where x = 1" {
		t.Errorf("true: %q", res.SQL)
	}
	if res := renderTmpl(t, tmpl, expr.Scope{}, qmark); res.SQL != "select 1 from t " {
		t.Errorf("nil-falsy: %q", res.SQL)
	}
	// non-nil non-bool -> error
	n, _ := parser.Parse(tmpl)
	if _, err := render.Render(n, expr.Scope{"flag": 5}, render.Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litFn}); err == nil {
		t.Error("non-bool if must error")
	}
}

func TestRender_For(t *testing.T) {
	// nil iterable -> zero iterations
	if res := renderTmpl(t, "x /*%for i in xs*/Y/*%end*/", expr.Scope{}, qmark); res.SQL != "x " {
		t.Errorf("nil for: %q", res.SQL)
	}
	// the body is emitted verbatim per element, with no text inserted between iterations
	if res := renderTmpl(t, "/*%for c in cols*/z/*%end*/", expr.Scope{"cols": []any{1, 2, 3}}, qmark); res.SQL != "zzz" {
		t.Errorf("verbatim body: %q", res.SQL)
	}
	// a comma list is anchored (a fixed first element) and each iteration leads with its comma
	res := renderTmpl(t, "0/*%for c in cols*/, z/*%end*/", expr.Scope{"cols": []any{1, 2, 3}}, qmark)
	if res.SQL != "0, z, z, z" {
		t.Errorf("leading-comma list: %q", res.SQL)
	}
	// non-nil non-iterable -> error
	n, _ := parser.Parse("/*%for i in xs*/Y/*%end*/")
	if _, err := render.Render(n, expr.Scope{"xs": 5}, render.Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litFn}); err == nil {
		t.Error("non-iterable for must error")
	}
}

// for saves and restores a pre-existing scope key shadowed by the loop variable.
func TestRender_ForRestoresScope(t *testing.T) {
	// A caller key named i is shadowed by the loop variable during the loop and restored after.
	n, _ := parser.Parse("/*%for i in xs*/z/*%end*/[/*^i*/'x']")
	res, err := render.Render(n, expr.Scope{"xs": []any{1, 2}, "i": "KEEP"},
		render.Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litFn})
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "zz[KEEP]" {
		t.Errorf("got %q, want %q (i restored after loop)", res.SQL, "zz[KEEP]")
	}
}

// for deletes a loop variable that had no pre-existing scope entry, so a later reference to
// that key resolves to nil again (here rendered as the litFn(nil) form).
func TestRender_ForDeletesScopeWhenAbsent(t *testing.T) {
	// No pre-existing i in scope: after the loop the key must be absent, not lingering as its
	// last iteration value, so the trailing /*^i*/ literal sees nil -> "null".
	res := renderTmpl(t, "/*%for i in xs*/z/*%end*/[/*^i*/'?']", expr.Scope{"xs": []any{1, 2}}, qmark)
	if res.SQL != "zz[null]" {
		t.Errorf("got %q, want %q (i absent after loop)", res.SQL, "zz[null]")
	}
}

// The placeholder counter stays gap-free even when an /*%if*/ branch that contains a bind is
// not taken: skipped binds must not consume a number.
func TestRender_PlaceholderCounterGapFreeAcrossUnreachedIf(t *testing.T) {
	tmpl := "select /*a*/0 /*%if flag*/, /*b*/0/*%end*/, /*c*/0"
	// flag=false: the /*b*/ bind is never reached, so a->$1 and c->$2 with no gap.
	res := renderTmpl(t, tmpl, expr.Scope{"a": 1, "c": 3, "flag": false}, dollar)
	if res.SQL != "select $1 , $2" || len(res.Args) != 2 {
		t.Errorf("false: SQL=%q Args=%#v", res.SQL, res.Args)
	}
	if !reflect.DeepEqual(res.Args, []any{1, 3}) {
		t.Errorf("false: Args=%#v", res.Args)
	}
	// flag=true: all three binds are reached, numbered contiguously.
	res = renderTmpl(t, tmpl, expr.Scope{"a": 1, "b": 2, "c": 3, "flag": true}, dollar)
	if res.SQL != "select $1 , $2, $3" || !reflect.DeepEqual(res.Args, []any{1, 2, 3}) {
		t.Errorf("true: SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

// Binds inside a /*%for*/ body are numbered across iterations. The list is anchored by a fixed
// first element (select 0) with each iteration leading with its own comma.
func TestRender_BindInsideFor(t *testing.T) {
	res := renderTmpl(t, "select 0/*%for i in xs*/, (/*i*/0)/*%end*/", expr.Scope{"xs": []any{1, 2, 3}}, dollar)
	if res.SQL != "select 0, ($1), ($2), ($3)" || !reflect.DeepEqual(res.Args, []any{1, 2, 3}) {
		t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

// /*%if*/ /*%elseif*/ /*%else*/ pick the first truthy branch, falling through to else.
func TestRender_IfElseifElse(t *testing.T) {
	tmpl := "/*%if a*/A/*%elseif b*/B/*%else*/C/*%end*/"
	if res := renderTmpl(t, tmpl, expr.Scope{"a": true, "b": false}, qmark); res.SQL != "A" {
		t.Errorf("a=true: got %q, want A", res.SQL)
	}
	if res := renderTmpl(t, tmpl, expr.Scope{"a": false, "b": true}, qmark); res.SQL != "B" {
		t.Errorf("a=false,b=true: got %q, want B", res.SQL)
	}
	if res := renderTmpl(t, tmpl, expr.Scope{"a": false, "b": false}, qmark); res.SQL != "C" {
		t.Errorf("both false: got %q, want C", res.SQL)
	}
}

// A scalar value under a paren test expands to a single-element list, not a scalar bind.
func TestRender_ScalarUnderParenTest(t *testing.T) {
	res := renderTmpl(t, "id in /*x*/(0)", expr.Scope{"x": 5}, qmark)
	if res.SQL != "id in (?)" || !reflect.DeepEqual(res.Args, []any{5}) {
		t.Errorf("SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

// Trailing content after the test literal is rendered verbatim after the placeholder/literal.
func TestRender_TrailingAfterTestLiteral(t *testing.T) {
	// bind: the ::int cast trails the placeholder.
	res := renderTmpl(t, "/*x*/0::int", expr.Scope{"x": 7}, dollar)
	if res.SQL != "$1::int" || !reflect.DeepEqual(res.Args, []any{7}) {
		t.Errorf("bind: SQL=%q Args=%#v", res.SQL, res.Args)
	}
	// literal: the ::text cast trails the inlined literal.
	res = renderTmpl(t, "/*^v*/'x'::text", expr.Scope{"v": 42}, qmark)
	if res.SQL != "42::text" || len(res.Args) != 0 {
		t.Errorf("literal: SQL=%q Args=%#v", res.SQL, res.Args)
	}
}

// evalErr fails on every Eval, standing in for an expression-language error.
type evalErr struct{}

func (evalErr) Eval(string, expr.Scope) (any, error) { return nil, fmt.Errorf("boom") }

// An evaluator error surfaces from Render wrapped with the "evaluating" context.
func TestRender_EvaluatorErrorPropagates(t *testing.T) {
	n, err := parser.Parse("a = /*x*/0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.Render(n, expr.Scope{}, render.Config{Evaluator: evalErr{}, Placeholder: qmark, Literal: litFn})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "evaluating") {
		t.Errorf("error %q does not mention evaluating", err)
	}
}

// M1: a literal's expression evaluation error surfaces from Render (visitLiteral).
func TestRender_LiteralEvalErrorPropagates(t *testing.T) {
	n, err := parser.Parse("/*^v*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.Render(n, expr.Scope{}, render.Config{Evaluator: evalErr{}, Placeholder: qmark, Literal: litFn})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "evaluating") {
		t.Errorf("error %q does not mention evaluating", err)
	}
}

// M2: an /*%if*/ condition evaluation error surfaces from Render (evalBool).
func TestRender_IfConditionEvalErrorPropagates(t *testing.T) {
	n, err := parser.Parse("/*%if flag*/x/*%end*/")
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.Render(n, expr.Scope{}, render.Config{Evaluator: evalErr{}, Placeholder: qmark, Literal: litFn})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "evaluating") {
		t.Errorf("error %q does not mention evaluating", err)
	}
}

// M3: a /*%for*/ iterable expression evaluation error surfaces from Render (visitFor).
func TestRender_ForExprEvalErrorPropagates(t *testing.T) {
	n, err := parser.Parse("/*%for i in xs*/Y/*%end*/")
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.Render(n, expr.Scope{}, render.Config{Evaluator: evalErr{}, Placeholder: qmark, Literal: litFn})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "evaluating") {
		t.Errorf("error %q does not mention evaluating", err)
	}
}

// selEval evaluates each key independently: keys mapping to a non-nil value return it, keys
// mapping to nil return an error. It stands in for an evaluator that succeeds on the leading
// /*%if*/ condition (boolean false) yet fails on a later /*%elseif*/ condition.
type selEval map[string]any

func (s selEval) Eval(expression string, _ expr.Scope) (any, error) {
	if v, ok := s[expression]; ok {
		if v == nil {
			return nil, fmt.Errorf("boom: %s", expression)
		}
		return v, nil
	}
	return nil, nil
}

// M4: an /*%elseif*/ condition error is not swallowed by chooseBranch — the leading if is a
// clean boolean false (not an error), so evaluation proceeds to the elseif, which errors.
func TestRender_ElseifConditionEvalErrorPropagates(t *testing.T) {
	n, err := parser.Parse("/*%if a*/A/*%elseif b*/B/*%end*/")
	if err != nil {
		t.Fatal(err)
	}
	// a -> boolean false (falsy, not an error); b -> nil which selEval turns into an error.
	_, err = render.Render(n, expr.Scope{}, render.Config{Evaluator: selEval{"a": false, "b": nil}, Placeholder: qmark, Literal: litFn})
	if err == nil {
		t.Fatal("expected an error from the elseif condition")
	}
	if !strings.Contains(err.Error(), "evaluating") {
		t.Errorf("error %q does not mention evaluating", err)
	}
}

// A literal-formatter error surfaces from Render wrapped with the "literal" context.
func TestRender_LiteralFormatterErrorWraps(t *testing.T) {
	litErr := func(any) (string, error) { return "", fmt.Errorf("bad value") }
	n, err := parser.Parse("/*^v*/'x'")
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.Render(n, expr.Scope{"v": 42}, render.Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litErr})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "literal") {
		t.Errorf("error %q does not mention literal", err)
	}
}

// ArgSpans point at the exact placeholder bytes for each arg, aligned with Args.
func TestRender_ArgSpansAlignWithArgs(t *testing.T) {
	res := renderTmpl(t, "a = /*x*/0 and b = /*y*/0", expr.Scope{"x": 1, "y": 2}, dollar)
	if res.SQL != "a = $1 and b = $2" || !reflect.DeepEqual(res.Args, []any{1, 2}) {
		t.Fatalf("SQL=%q Args=%#v", res.SQL, res.Args)
	}
	want := [][2]int{{4, 6}, {15, 17}}
	if !reflect.DeepEqual(res.ArgSpans, want) {
		t.Fatalf("ArgSpans=%#v want %#v", res.ArgSpans, want)
	}
	for i, sp := range res.ArgSpans {
		if got := res.SQL[sp[0]:sp[1]]; got != "$"+strconv.Itoa(i+1) {
			t.Errorf("span %d yields %q", i, got)
		}
	}
}
