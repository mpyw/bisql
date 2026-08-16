// Package exprlang is bisql's built-in expression evaluator for directive expressions
// (the e in /*%if e*/, the bind expression in /* e */, and so on).
//
// It is a thin wrapper around github.com/expr-lang/expr, the de-facto Go expression
// language: safe, bytecode-compiled, and rich (property/method access, comparisons,
// boolean logic, nil checks, the "in" operator, len/collection helpers, optional
// chaining a?.b, nil-coalescing ??). Compiled programs are cached per expression string.
//
// Note on syntax vs. Komapper: expressions live inside SQL comments and are never sent to
// the database, so their syntax has no bearing on the 2-way (runnable-as-is) property.
// The one visible difference from Komapper's Kotlin-flavored language is that the null
// literal is written "nil" (expr-lang's spelling), not "null". Callers who need a
// different expression dialect can plug a custom evaluator via bisql.WithEvaluator.
package exprlang

import (
	"sync"

	goexpr "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/mpyw/bisql/expr"
)

// Default is bisql's built-in evaluator. It satisfies expr.Evaluator.
//
// The zero value is ready to use and safe for concurrent use.
type Default struct {
	cache sync.Map // expression string -> *vm.Program
}

// Eval evaluates expression against scope and returns the resulting Go value.
func (d *Default) Eval(expression string, scope expr.Scope) (any, error) {
	prog, err := d.compile(expression)
	if err != nil {
		return nil, err
	}
	var env any
	if scope != nil {
		env = map[string]any(scope)
	}
	return vm.Run(prog, env)
}

func (d *Default) compile(expression string) (*vm.Program, error) {
	if p, ok := d.cache.Load(expression); ok {
		return p.(*vm.Program), nil
	}
	// AllowUndefinedVariables: identifiers absent from the scope evaluate to nil rather
	// than failing, so idioms like `name != nil` work when the key is simply missing.
	prog, err := goexpr.Compile(expression, goexpr.AllowUndefinedVariables())
	if err != nil {
		return nil, err
	}
	actual, _ := d.cache.LoadOrStore(expression, prog)
	return actual.(*vm.Program), nil
}
