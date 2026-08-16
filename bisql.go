package bisql

import (
	"fmt"
	"reflect"

	"github.com/mpyw/bisql/dialect"
	"github.com/mpyw/bisql/expr"
	"github.com/mpyw/bisql/internal/exprlang"
	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
	"github.com/mpyw/bisql/internal/sqltmpl/preprocess"
	"github.com/mpyw/bisql/internal/sqltmpl/render"
)

// Template is a parsed template. It is immutable and safe for concurrent Build calls.
type Template struct {
	root      ast.Node
	dialect   dialect.Dialect
	evaluator expr.Evaluator
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

// Option adjusts Parse / Expand.
type Option func(*config)

type config struct {
	dialect   dialect.Dialect
	evaluator expr.Evaluator
	loader    Loader
}

func defaultConfig() config {
	return config{dialect: dialect.MySQL, evaluator: &exprlang.Default{}}
}

// WithDialect sets the dialect used for placeholder generation (default: MySQL).
func WithDialect(d dialect.Dialect) Option { return func(c *config) { c.dialect = d } }

// WithEvaluator swaps the expression evaluator (default: the built-in one).
func WithEvaluator(e expr.Evaluator) Option { return func(c *config) { c.evaluator = e } }

// WithLoader sets how /*%! @include name */ directives are resolved. There is no default:
// a template that uses @include must be parsed with a Loader (RegistryLoader, FSLoader, a
// LoaderFunc, or your own), otherwise @include is an error.
func WithLoader(l Loader) Option { return func(c *config) { c.loader = l } }

// resolver returns the preprocess resolver for c's loader (or one that rejects @include).
func (c config) resolver() func(string) (string, error) {
	if c.loader == nil {
		return func(name string) (string, error) {
			return "", fmt.Errorf("bisql: @include %q requires a Loader (pass bisql.WithLoader)", name)
		}
	}
	return c.loader.Load
}

// Parse parses a template string, expanding any /*%! @include ... */ against the loader set
// with WithLoader (absent a loader, @include is an error).
func Parse(src string, opts ...Option) (*Template, error) {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	expanded, err := preprocess.Expand(src, c.resolver())
	if err != nil {
		return nil, err
	}
	root, err := parser.Parse(expanded)
	if err != nil {
		return nil, err
	}
	return &Template{root: root, dialect: c.dialect, evaluator: c.evaluator}, nil
}

// Expand runs only the @include preprocessor and returns the fully expanded, still-2-way
// template text (useful to snapshot or run through EXPLAIN ahead of time).
func Expand(src string, opts ...Option) (string, error) {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	return preprocess.Expand(src, c.resolver())
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
	})
	if err != nil {
		return Statement{}, err
	}
	return Statement{SQL: res.SQL, Args: res.Args, SQLWithArgs: res.SQLWithArgs}, nil
}

// toScope converts params into an expression scope. It accepts nil, a map[string]any, an
// expr.Scope, or a struct (or pointer to one), whose exported fields become scope entries
// keyed by field name.
func toScope(params any) (expr.Scope, error) {
	switch p := params.(type) {
	case nil:
		return expr.Scope{}, nil
	case map[string]any:
		return p, nil
	case expr.Scope:
		return p, nil
	}
	rv := reflect.ValueOf(params)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return expr.Scope{}, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		return structScope(rv), nil
	}
	return nil, fmt.Errorf("bisql: cannot use %T as parameters (want map[string]any, expr.Scope, or a struct)", params)
}

// structScope flattens a struct's exported fields into a scope. Fields of anonymous
// (embedded) structs are promoted to their bare names, matching Go's field promotion and
// encoding/json: shallower fields win over deeper ones (breadth-first). An embedded struct
// is also kept under its own type name so it stays reachable qualified (e.g. Base.Field).
func structScope(rv reflect.Value) expr.Scope {
	scope := expr.Scope{}
	level := []reflect.Value{rv}
	for len(level) > 0 {
		var next []reflect.Value
		for _, sv := range level {
			t := sv.Type()
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				fv := sv.Field(i)
				if f.Anonymous {
					ev := fv
					for ev.Kind() == reflect.Pointer && !ev.IsNil() {
						ev = ev.Elem()
					}
					if ev.Kind() == reflect.Struct {
						next = append(next, ev)
					}
				}
				if f.IsExported() {
					if _, seen := scope[f.Name]; !seen {
						scope[f.Name] = fv.Interface()
					}
				}
			}
		}
		level = next
	}
	return scope
}
