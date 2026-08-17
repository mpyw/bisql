package bisql

import (
	"fmt"
	"io/fs"
	"reflect"
	"strings"

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
//   - SQL:  placeholder form (for execution)
//   - Args: bind arguments
//
// The values-embedded form is available via the SQLWithArgs method (computed on demand).
type Statement struct {
	SQL  string
	Args []any

	lit   dialect.Literal // for the lazily-built SQLWithArgs
	spans [][2]int        // placeholder byte ranges in SQL, aligned with Args
}

// SQLWithArgs returns the values-embedded form of the statement — Args inlined as SQL
// literals — for snapshots and review. Never execute it: literals are not a substitute for
// bound parameters (injection). It is computed on demand from Args, so a Statement you only
// execute never pays for it; formatting is best-effort (a value the dialect cannot format
// falls back to Go's %v).
func (s Statement) SQLWithArgs() string {
	if len(s.spans) == 0 {
		return s.SQL
	}
	var b strings.Builder
	prev := 0
	for i, sp := range s.spans {
		b.WriteString(s.SQL[prev:sp[0]])
		lit, err := s.lit(s.Args[i])
		if err != nil {
			lit = fmt.Sprintf("%v", s.Args[i])
		}
		b.WriteString(lit)
		prev = sp[1]
	}
	b.WriteString(s.SQL[prev:])
	return b.String()
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

// WithStackedLoader resolves fragments by trying loaders in order, falling through to the
// next whenever one reports the fragment is not found (see ErrNotFound); any other error
// aborts. It is shorthand for WithLoader(NewStackedLoader(loaders...)).
func WithStackedLoader(loaders ...Loader) Option {
	return WithLoader(NewStackedLoader(loaders...))
}

// resolver returns the preprocess resolver for c's loader (or one that rejects @include).
func (c config) resolver() func(string) (string, error) {
	if c.loader == nil {
		return func(name string) (string, error) {
			return "", fmt.Errorf("bisql: @include %q requires a Loader (pass bisql.WithLoader)", name)
		}
	}
	return c.loader.Load
}

// Parser holds parse-time configuration (dialect, evaluator, loader) so it can be built once
// with NewParser and reused to parse many templates without repeating options. It is
// immutable and safe for concurrent use.
type Parser struct {
	c config
}

// NewParser returns a Parser configured by opts (dialect, evaluator, loader).
func NewParser(opts ...Option) *Parser {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	return &Parser{c: c}
}

// Parse parses a template string, expanding any /*%! @include ... */ against the parser's
// loader (absent a loader, @include is an error).
func (p *Parser) Parse(src string) (*Template, error) {
	expanded, err := preprocess.Expand(src, p.c.resolver())
	if err != nil {
		return nil, err
	}
	root, err := parser.Parse(expanded)
	if err != nil {
		return nil, err
	}
	return &Template{root: root, dialect: p.c.dialect, evaluator: p.c.evaluator}, nil
}

// Expand runs only the @include preprocessor and returns the fully expanded, still-2-way
// template text (useful to snapshot or run through EXPLAIN ahead of time).
func (p *Parser) Expand(src string) (string, error) {
	return preprocess.Expand(src, p.c.resolver())
}

// ParseFile reads the template named by name from fsys and parses it. Unless the parser was
// configured with an explicit loader, /*%! @include ... */ directives are resolved from the
// same fsys (as an FSLoader), so the root template and its fragments live together in one
// file tree; include names are paths relative to the root of fsys.
func (p *Parser) ParseFile(fsys fs.FS, name string) (*Template, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("bisql: reading template %q: %w", name, err)
	}
	return p.forFS(fsys).Parse(string(src))
}

// ExpandFile reads the template named by name from fsys and returns its expanded text (see
// Expand and ParseFile).
func (p *Parser) ExpandFile(fsys fs.FS, name string) (string, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", fmt.Errorf("bisql: reading template %q: %w", name, err)
	}
	return p.forFS(fsys).Expand(string(src))
}

// forFS returns a parser whose loader defaults to an FSLoader over fsys when none was
// configured, leaving an explicitly-set loader untouched. The receiver is not mutated.
func (p *Parser) forFS(fsys fs.FS) *Parser {
	if p.c.loader != nil {
		return p
	}
	c := p.c
	c.loader = NewFSLoader(fsys)
	return &Parser{c: c}
}

// Parse is a shortcut for NewParser(opts...).Parse(src); use NewParser to reuse one
// configuration across many templates.
func Parse(src string, opts ...Option) (*Template, error) {
	return NewParser(opts...).Parse(src)
}

// Expand is a shortcut for NewParser(opts...).Expand(src).
func Expand(src string, opts ...Option) (string, error) {
	return NewParser(opts...).Expand(src)
}

// ParseFile is a shortcut for NewParser(opts...).ParseFile(fsys, name).
func ParseFile(fsys fs.FS, name string, opts ...Option) (*Template, error) {
	return NewParser(opts...).ParseFile(fsys, name)
}

// ExpandFile is a shortcut for NewParser(opts...).ExpandFile(fsys, name).
func ExpandFile(fsys fs.FS, name string, opts ...Option) (string, error) {
	return NewParser(opts...).ExpandFile(fsys, name)
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
	return Statement{SQL: res.SQL, Args: res.Args, lit: t.dialect.Literal(), spans: res.ArgSpans}, nil
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
