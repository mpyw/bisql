package exprlang

import (
	"testing"

	"github.com/mpyw/bisql/pkg/expr"
)

func eval(t *testing.T, e string, scope expr.Scope) any {
	t.Helper()
	v, err := Default{}.Eval(e, scope)
	if err != nil {
		t.Fatalf("Eval(%q): %v", e, err)
	}
	return v
}

func evalErr(t *testing.T, e string, scope expr.Scope) {
	t.Helper()
	if _, err := (Default{}).Eval(e, scope); err == nil {
		t.Fatalf("Eval(%q): expected error", e)
	}
}

type inner struct{ N int }
type outer struct {
	Name  string
	Inner inner
}

func (o outer) Greet(who string) string { return "hi " + who + " from " + o.Name }

func TestEval_literals(t *testing.T) {
	if v := eval(t, "null", nil); v != nil {
		t.Errorf("null: %v", v)
	}
	if v := eval(t, "true", nil); v != true {
		t.Errorf("true: %v", v)
	}
	if v := eval(t, "false", nil); v != false {
		t.Errorf("false: %v", v)
	}
	if v := eval(t, `"abc"`, nil); v != "abc" {
		t.Errorf("string: %v", v)
	}
	if v := eval(t, "42", nil); v != int64(42) {
		t.Errorf("int: %v (%T)", v, v)
	}
	if v := eval(t, "3.5", nil); v != 3.5 {
		t.Errorf("float: %v (%T)", v, v)
	}
}

func TestEval_comparisons(t *testing.T) {
	cases := []struct {
		e    string
		want bool
	}{
		{"1 == 1", true}, {"1 == 2", false},
		{"1 != 2", true}, {"1 != 1", false},
		{"2 > 1", true}, {"1 > 2", false},
		{"1 < 2", true}, {"2 < 1", false},
		{"2 >= 2", true}, {"2 >= 3", false},
		{"2 <= 2", true}, {"3 <= 2", false},
		{`"a" == "a"`, true}, {`"a" != "b"`, true},
		{"1 < 2.0", true}, {"2.0 == 2", true},
	}
	for _, c := range cases {
		if v := eval(t, c.e, nil); v != c.want {
			t.Errorf("%q = %v, want %v", c.e, v, c.want)
		}
	}
}

func TestEval_logical(t *testing.T) {
	cases := []struct {
		e    string
		want bool
	}{
		{"true && true", true}, {"true && false", false},
		{"false || true", true}, {"false || false", false},
		{"!true", false}, {"!false", true},
		{"!(1 == 2)", true},
		{"1 == 1 && 2 == 2", true},
		{"1 == 2 || 3 == 3", true},
	}
	for _, c := range cases {
		if v := eval(t, c.e, nil); v != c.want {
			t.Errorf("%q = %v, want %v", c.e, v, c.want)
		}
	}
}

func TestEval_nullChecks(t *testing.T) {
	if v := eval(t, "name != null", expr.Scope{"name": "aaa"}); v != true {
		t.Errorf("name!=null with value: %v", v)
	}
	if v := eval(t, "name != null", expr.Scope{"name": nil}); v != false {
		t.Errorf("name!=null with nil: %v", v)
	}
	if v := eval(t, "name == null", expr.Scope{"name": nil}); v != true {
		t.Errorf("name==null with nil: %v", v)
	}
}

func TestEval_property(t *testing.T) {
	scope := expr.Scope{
		"o": outer{Name: "Bob", Inner: inner{N: 7}},
		"m": map[string]any{"k": "v", "nested": map[string]any{"x": 1}},
	}
	if v := eval(t, "o.Name", scope); v != "Bob" {
		t.Errorf("o.Name: %v", v)
	}
	if v := eval(t, "o.Inner.N", scope); v != 7 {
		t.Errorf("o.Inner.N: %v", v)
	}
	if v := eval(t, "m.k", scope); v != "v" {
		t.Errorf("m.k: %v", v)
	}
	if v := eval(t, "m.nested.x", scope); v != 1 {
		t.Errorf("m.nested.x: %v", v)
	}
	// case-insensitive field fallback: o.name resolves to field Name
	if v := eval(t, "o.name", scope); v != "Bob" {
		t.Errorf("o.name (ci): %v", v)
	}
}

func TestEval_safeCall(t *testing.T) {
	scope := expr.Scope{"o": nil}
	if v := eval(t, "o?.Name", scope); v != nil {
		t.Errorf("safe call on nil should be nil, got %v", v)
	}
	// plain call on nil errors
	evalErr(t, "o.Name", scope)
}

func TestEval_method(t *testing.T) {
	scope := expr.Scope{"o": outer{Name: "Bob"}}
	if v := eval(t, `o.Greet("x")`, scope); v != "hi x from Bob" {
		t.Errorf("method: %v", v)
	}
}

func TestEval_errors(t *testing.T) {
	evalErr(t, "1 +", nil)       // dangling
	evalErr(t, "(1 == 1", nil)   // unbalanced paren
	evalErr(t, "1 && 2", nil)    // non-bool operands to &&
	evalErr(t, "unknown.x", nil) // unknown identifier
	evalErr(t, `"a" > 1`, nil)   // incomparable types
}
