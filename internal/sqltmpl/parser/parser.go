// Package parser builds the shallow structural tree from a SQL template, using a
// reducer-stack strategy to fold clauses, operators, and blocks
// (see docs/komapper-analysis.md).
package parser

import (
	"errors"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// ErrNotImplemented is returned while the parser is scaffolding.
var ErrNotImplemented = errors.New("bisql/parser: not implemented yet (M2)")

// Parse turns a template string into the tree.
//
// TODO(M2): implement.
func Parse(src string) (ast.Node, error) {
	return nil, ErrNotImplemented
}
