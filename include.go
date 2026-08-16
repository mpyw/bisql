package bisql

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/mpyw/bisql/internal/sqltmpl/parser"
	"github.com/mpyw/bisql/internal/sqltmpl/preprocess"
)

// Loader manages the fragments referenced by @include directives.
//
// An /*%! @include name */ directive is expanded textually (before lexing) into the named
// fragment's raw text, recursively — a preprocessing step that yields a fully expanded,
// still-2-way template. Register the fragments up front (Register / LoadFS), then Parse.
// The resulting Template is immutable and safe for concurrent Build calls.
type Loader struct {
	cfg       config
	fragments map[string]string
}

// NewLoader creates a Loader.
func NewLoader(opts ...Option) *Loader {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	return &Loader{cfg: c, fragments: map[string]string{}}
}

// Register registers a named fragment.
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

func (l *Loader) resolve(name string) (string, error) {
	src, ok := l.fragments[name]
	if !ok {
		return "", fmt.Errorf("bisql: unknown @include fragment %q", name)
	}
	return src, nil
}

// Expand runs the @include preprocessor and returns the fully expanded template text (the
// resolved, still-2-way SQL). Useful to snapshot or run through EXPLAIN ahead of time.
func (l *Loader) Expand(src string) (string, error) {
	return preprocess.Expand(src, l.resolve)
}

// Parse expands @include directives against this Loader's fragments, then parses.
func (l *Loader) Parse(src string) (*Template, error) {
	expanded, err := preprocess.Expand(src, l.resolve)
	if err != nil {
		return nil, err
	}
	root, err := parser.Parse(expanded)
	if err != nil {
		return nil, err
	}
	return &Template{root: root, dialect: l.cfg.dialect, evaluator: l.cfg.evaluator}, nil
}
