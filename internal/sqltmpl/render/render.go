// Package render evaluates the template tree into (SQL, args). This is the heart of
// 2-way SQL: it propagates an "available" flag to drop clauses that become empty and
// leading AND/OR left dangling. See docs/komapper-analysis.md and docs/design.md.
package render

import (
	"errors"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/pkg/dialect"
	"github.com/mpyw/bisql/pkg/expr"
)

// ErrNotImplemented is returned while the renderer is scaffolding.
var ErrNotImplemented = errors.New("bisql/render: not implemented yet (M4)")

// Result is the rendered statement.
type Result struct {
	SQL  string
	Args []any
}

// Render evaluates the tree with the given scope, evaluator, and placeholder.
//
// TODO(M4): implement the available-flag evaluator.
func Render(n ast.Node, scope expr.Scope, ev expr.Evaluator, ph dialect.Placeholder) (Result, error) {
	return Result{}, ErrNotImplemented
}
