package render

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
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

func renderTmpl(t *testing.T, tmpl string, scope expr.Scope, ph func(int, string) string) Result {
	t.Helper()
	n, err := parser.Parse(tmpl)
	if err != nil {
		t.Fatalf("parse %q: %v", tmpl, err)
	}
	res, err := Render(n, scope, Config{Evaluator: stubEval{}, Placeholder: ph, Literal: litFn})
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
	if _, err := Render(n, expr.Scope{"flag": 5}, Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litFn}); err == nil {
		t.Error("non-bool if must error")
	}
}

func TestRender_For(t *testing.T) {
	// nil iterable -> zero iterations
	if res := renderTmpl(t, "x /*%for i in xs*/Y/*%end*/", expr.Scope{}, qmark); res.SQL != "x " {
		t.Errorf("nil for: %q", res.SQL)
	}
	// the `: 'sep'` clause emits the separator between iterations only (no trailing separator)
	res := renderTmpl(t, "/*%for c in cols : ', '*/z/*%end*/", expr.Scope{"cols": []any{1, 2, 3}}, qmark)
	if res.SQL != "z, z, z" {
		t.Errorf("separator list: %q", res.SQL)
	}
	// non-nil non-iterable -> error
	n, _ := parser.Parse("/*%for i in xs*/Y/*%end*/")
	if _, err := Render(n, expr.Scope{"xs": 5}, Config{Evaluator: stubEval{}, Placeholder: qmark, Literal: litFn}); err == nil {
		t.Error("non-iterable for must error")
	}
}

// for saves and restores a pre-existing scope key shadowed by the loop variable.
func TestRender_ForRestoresScope(t *testing.T) {
	// A caller key named i is shadowed by the loop variable during the loop and restored after.
	n, _ := parser.Parse("/*%for i in xs*/z/*%end*/[/*^i*/'x']")
	res, err := Render(n, expr.Scope{"xs": []any{1, 2}, "i": "KEEP"},
		Config{Evaluator: keyEval{}, Placeholder: qmark, Literal: litFn})
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "zz[KEEP]" {
		t.Errorf("got %q, want %q (i restored after loop)", res.SQL, "zz[KEEP]")
	}
}

// keyEval resolves an expression as a scope key.
type keyEval struct{}

func (keyEval) Eval(e string, s expr.Scope) (any, error) { return s[e], nil }
