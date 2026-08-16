// Package exprlang is bisql's built-in expression evaluator for directive expressions
// (the e in /*%if e*/, the bind expression in /* e */, etc.).
//
// It is a small, Go-idiomatic language: literals (null/true/false/string/int/float),
// comparisons (== != > < >= <=), logical operators (! && ||), parentheses, property access
// (a.b.c), safe calls (a?.b), and method/function calls. Members are resolved against
// map[string]any keys and struct fields/methods via reflection.
//
// Deviations from Komapper: Kotlin-specific constructs (class references @FQCN@, the is/as
// type operators, numeric type suffixes L/F/D/B) are intentionally omitted; they do not map
// to Go. Callers needing them can plug a custom evaluator via bisql.WithEvaluator.
package exprlang

import (
	"fmt"

	"github.com/mpyw/bisql/pkg/expr"
)

// Default is bisql's built-in evaluator. It satisfies expr.Evaluator.
type Default struct{}

// Eval evaluates expression against scope.
func (Default) Eval(expression string, scope expr.Scope) (any, error) {
	toks, err := lex(expression)
	if err != nil {
		return nil, err
	}
	p := &evaluator{toks: toks, scope: scope}
	v, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("bisql/exprlang: unexpected token %q", p.cur().text)
	}
	return v, nil
}
