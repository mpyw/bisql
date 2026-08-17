package exprlang_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/exprlang"
)

func eval(t *testing.T, e string, scope expr.Scope) any {
	t.Helper()
	v, err := (&exprlang.Default{}).Eval(e, scope)
	if err != nil {
		t.Fatalf("Eval(%q): %v", e, err)
	}
	return v
}

func evalErr(t *testing.T, e string, scope expr.Scope) {
	t.Helper()
	if _, err := (&exprlang.Default{}).Eval(e, scope); err == nil {
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
	if v := eval(t, "nil", nil); v != nil {
		t.Errorf("nil: %v", v)
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
	if v := eval(t, "42", nil); v != 42 {
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

func TestEval_nilChecks(t *testing.T) {
	if v := eval(t, "name != nil", expr.Scope{"name": "aaa"}); v != true {
		t.Errorf("name!=nil with value: %v", v)
	}
	if v := eval(t, "name != nil", expr.Scope{"name": nil}); v != false {
		t.Errorf("name!=nil with nil: %v", v)
	}
	if v := eval(t, "name == nil", expr.Scope{"name": nil}); v != true {
		t.Errorf("name==nil with nil: %v", v)
	}
	// A key that is simply absent evaluates to nil (AllowUndefinedVariables).
	if v := eval(t, "name == nil", expr.Scope{}); v != true {
		t.Errorf("name==nil with absent key: %v", v)
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
}

func TestEval_optionalChaining(t *testing.T) {
	scope := expr.Scope{"o": nil}
	if v := eval(t, "o?.Name", scope); v != nil {
		t.Errorf("optional chaining on nil should be nil, got %v", v)
	}
}

// TestEval_optionalChainingValue covers the value path of a?.b: on a NON-nil operand the
// chain resolves the member just like a plain property access (the nil path is covered above).
func TestEval_optionalChainingValue(t *testing.T) {
	scope := expr.Scope{
		"o": outer{Name: "Bob"},
		"m": map[string]any{"Name": "Kate"},
	}
	if v := eval(t, "o?.Name", scope); v != "Bob" {
		t.Errorf("o?.Name on non-nil struct: %v", v)
	}
	if v := eval(t, "m?.Name", scope); v != "Kate" {
		t.Errorf("m?.Name on non-nil map: %v", v)
	}
}

// TestEval_nilCoalescing covers the ?? operator: it yields the right operand when the left is
// nil, and the left operand otherwise.
func TestEval_nilCoalescing(t *testing.T) {
	if v := eval(t, "a ?? 7", expr.Scope{"a": nil}); v != 7 {
		t.Errorf("a ?? 7 with a=nil: %v", v)
	}
	if v := eval(t, "a ?? 7", expr.Scope{"a": 3}); v != 3 {
		t.Errorf("a ?? 7 with a=3: %v", v)
	}
}

// TestEval_nullVsNilFootgun pins the null-vs-nil footgun. expr-lang's null literal is spelled
// "nil"; a bare "null" is not a literal at all but an undefined variable, which — because of
// AllowUndefinedVariables — resolves to nil. As a result "name != null" and "name != nil"
// behave identically across every scope, which is exactly the trap: "null" silently works and
// so masks the fact that the canonical (and only real) spelling is "nil".
func TestEval_nullVsNilFootgun(t *testing.T) {
	scopes := []expr.Scope{
		{"name": "aaa"},
		{"name": nil},
		{}, // absent key
	}
	for _, sc := range scopes {
		gotNull := eval(t, "name != null", sc)
		gotNil := eval(t, "name != nil", sc)
		if gotNull != gotNil {
			t.Errorf("name != null (%v) and name != nil (%v) diverged for scope %v", gotNull, gotNil, sc)
		}
	}
	// "null" is itself just an undefined variable, i.e. nil.
	if v := eval(t, "null == nil", expr.Scope{}); v != true {
		t.Errorf("null == nil: %v", v)
	}
}

func TestEval_method(t *testing.T) {
	scope := expr.Scope{"o": outer{Name: "Bob"}}
	if v := eval(t, `o.Greet("x")`, scope); v != "hi x from Bob" {
		t.Errorf("method: %v", v)
	}
}

func TestEval_collections(t *testing.T) {
	scope := expr.Scope{"xs": []int{1, 2, 3}}
	if v := eval(t, "1 in xs", scope); v != true {
		t.Errorf("in operator: %v", v)
	}
	if v := eval(t, "len(xs)", scope); v != 3 {
		t.Errorf("len: %v", v)
	}
}

func TestEval_errors(t *testing.T) {
	evalErr(t, "1 +", nil)     // dangling
	evalErr(t, "(1 == 1", nil) // unbalanced paren
	evalErr(t, `"a" > 1`, nil) // incompatible types
}

func TestEval_cacheReuse(t *testing.T) {
	d := &exprlang.Default{}
	for i := 0; i < 3; i++ {
		v, err := d.Eval("a + 1", expr.Scope{"a": i})
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if v != i+1 {
			t.Errorf("a+1 with a=%d: %v", i, v)
		}
	}
}

// TestEval_concurrentSameExpression backs the "safe for concurrent use" claim: many goroutines
// hammer one shared Default with the same expression string (so they contend on the compiled-
// program cache) while each supplies its own scope. Run under -race to exercise the cache; this
// stays in the external package and never touches unexported cache internals.
func TestEval_concurrentSameExpression(t *testing.T) {
	t.Parallel()
	d := &exprlang.Default{}
	const goroutines = 32
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				v, err := d.Eval("a + 1", expr.Scope{"a": base})
				if err != nil {
					errs <- fmt.Errorf("Eval: %w", err)
					return
				}
				if v != base+1 {
					errs <- fmt.Errorf("a+1 with a=%d: got %v", base, v)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
