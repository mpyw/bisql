// Package exprlang is bisql's built-in expression language: a small evaluator that
// resolves map keys / struct fields / methods via reflection.
//
// TODO(M3): implement. This package will be split into token/ast/lexer/parser/eval
// sub-packages once it grows, following the repository's fine-grained package policy
// (see CLAUDE.md).
package exprlang

import (
	"errors"

	"github.com/mpyw/bisql/pkg/expr"
)

// Default is the built-in evaluator. It satisfies expr.Evaluator.
type Default struct{}

var errNotImplemented = errors.New("bisql/exprlang: default evaluator not implemented yet (M3)")

// Eval evaluates an expression.
func (Default) Eval(expression string, scope expr.Scope) (any, error) {
	return nil, errNotImplemented
}
