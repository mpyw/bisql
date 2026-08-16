package bisql

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
)

// Loader manages the fragments referenced by partials (/*> name */).
//
// bisql resolves a partial by re-parsing the named fragment into nodes and splicing it into
// the tree recursively, so /*%if*/, binds, and nested partials/embeds inside the fragment
// all work. This is the same recursive-expansion machinery embedded values (/*# */) use;
// the only difference is the source — a static, registered fragment here vs. a runtime
// scope expression for an embedded value. See docs/design.md ("include design").
//
// Register the fragments up front (via Register or LoadFS), then call Parse; the resulting
// Template is immutable and safe for concurrent Build calls.
type Loader struct {
	cfg       config
	fragments map[string]string
	cache     sync.Map // name -> ast.Node (parsed fragment)
}

// NewLoader creates a Loader.
func NewLoader(opts ...Option) *Loader {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	return &Loader{cfg: c, fragments: map[string]string{}}
}

// Register registers a named fragment template.
func (l *Loader) Register(name, template string) { l.fragments[name] = template }

// LoadFS loads files matching glob from fsys and registers each under its path with the
// extension removed as the fragment name (e.g. "sql/active.sql" -> "sql/active").
func (l *Loader) LoadFS(fsys fs.FS, glob string) error {
	matches, err := fs.Glob(fsys, glob)
	if err != nil {
		return fmt.Errorf("bisql: LoadFS glob %q: %w", glob, err)
	}
	for _, m := range matches {
		b, err := fs.ReadFile(fsys, m)
		if err != nil {
			return fmt.Errorf("bisql: LoadFS read %q: %w", m, err)
		}
		name := strings.TrimSuffix(m, path.Ext(m))
		l.Register(name, string(b))
	}
	return nil
}

// resolve parses (and caches) the named fragment into its tree. Partials nested inside a
// fragment are left as Partial nodes and resolved lazily at render time, where cycle
// detection lives.
func (l *Loader) resolve(name string) (ast.Node, error) {
	if n, ok := l.cache.Load(name); ok {
		return n.(ast.Node), nil
	}
	src, ok := l.fragments[name]
	if !ok {
		return nil, fmt.Errorf("bisql: unknown partial %q", name)
	}
	n, err := parser.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("bisql: parsing partial %q: %w", name, err)
	}
	actual, _ := l.cache.LoadOrStore(name, n)
	return actual.(ast.Node), nil
}

// Parse parses a template, wiring partial resolution against this Loader's fragments.
func (l *Loader) Parse(src string) (*Template, error) {
	root, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return &Template{
		root:      root,
		dialect:   l.cfg.dialect,
		evaluator: l.cfg.evaluator,
		resolve:   l.resolve,
	}, nil
}
