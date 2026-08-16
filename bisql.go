package bisql

import (
	"github.com/mpyw/bisql/internal/exprlang"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
	"github.com/mpyw/bisql/internal/sqltmpl/render"
	"github.com/mpyw/bisql/pkg/dialect"
	"github.com/mpyw/bisql/pkg/expr"
)

// Template is a parsed template. It is immutable and safe for concurrent Build calls.
type Template struct {
	root      ast.Node
	dialect   dialect.Dialect
	evaluator expr.Evaluator
	// resolve resolves partials (/*> name */) to their parsed trees; nil when the template
	// was produced by Parse (no Loader) so any partial is an error.
	resolve func(name string) (ast.Node, error)
}

// Statement is the result of Build.
//
//   - SQL:         placeholder form (for execution)
//   - Args:        bind arguments
//   - SQLWithArgs: values-embedded form (for snapshots/review; do not execute)
type Statement struct {
	SQL         string
	Args        []any
	SQLWithArgs string
}

// Option adjusts Parse / NewLoader.
type Option func(*config)

type config struct {
	dialect   dialect.Dialect
	evaluator expr.Evaluator
}

func defaultConfig() config {
	return config{dialect: dialect.MySQL, evaluator: &exprlang.Default{}}
}

// WithDialect sets the dialect used for placeholder generation (default: MySQL).
func WithDialect(d dialect.Dialect) Option { return func(c *config) { c.dialect = d } }

// WithEvaluator swaps the expression evaluator (default: the built-in one).
func WithEvaluator(e expr.Evaluator) Option { return func(c *config) { c.evaluator = e } }

// Parse parses a template string. Use Loader.Parse when the template uses partials
// (/*> name */).
func Parse(src string, opts ...Option) (*Template, error) {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	root, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return &Template{root: root, dialect: c.dialect, evaluator: c.evaluator}, nil
}

// Build assembles (SQL, Args) from the given parameters, which may be a map[string]any,
// an expr.Scope, or a struct.
func (t *Template) Build(params any) (Statement, error) {
	scope, err := toScope(params)
	if err != nil {
		return Statement{}, err
	}
	res, err := render.Render(t.root, scope, render.Config{
		Evaluator:   t.evaluator,
		Placeholder: t.dialect.Placeholder(),
		Literal:     t.dialect.Literal(),
		Resolve:     t.resolve,
	})
	if err != nil {
		return Statement{}, err
	}
	// TODO(M6): produce SQLWithArgs (values-embedded form).
	return Statement{SQL: res.SQL, Args: res.Args}, nil
}

// toScope converts params into an expression scope.
//
// TODO(M3): expand structs into fields via reflection. Only maps are handled for now.
func toScope(params any) (expr.Scope, error) {
	switch p := params.(type) {
	case nil:
		return expr.Scope{}, nil
	case map[string]any:
		return expr.Scope(p), nil
	case expr.Scope:
		return p, nil
	default:
		return nil, ErrNotImplemented
	}
}
