package bisql

import "io/fs"

// Loader manages the fragments referenced by partials (/*> name */).
//
// bisql resolves a partial by re-parsing the named fragment into nodes and splicing it
// into the tree. Unlike Komapper's /*# */ (raw-text embed, not re-parsed), /*%if*/ and
// binds inside the fragment work. See docs/design.md ("include design").
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

// Register registers a named fragment template.
func (l *Loader) Register(name, template string) { l.fragments[name] = template }

// LoadFS loads files matching glob from fsys and registers each under its path (without
// extension) as the fragment name.
//
// TODO(M5): implement.
func (l *Loader) LoadFS(fsys fs.FS, glob string) error { return ErrNotImplemented }

// Parse parses a template, resolving partials along the way.
//
// TODO(M5): resolve /*> name */ by re-parsing and splicing; detect cycles and missing
// fragments.
func (l *Loader) Parse(src string) (*Template, error) { return nil, ErrNotImplemented }
