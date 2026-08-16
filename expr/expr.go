// Package expr defines the pluggable evaluator for expressions inside directives
// (e.g. the e in /*%if e*/). The built-in default lives in internal/exprlang; callers can
// supply their own via bisql.WithEvaluator.
package expr

// Scope is the set of variables visible during expression evaluation.
type Scope map[string]any

// Evaluator evaluates an expression string against a scope.
type Evaluator interface {
	Eval(expression string, scope Scope) (any, error)
}
