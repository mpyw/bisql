package bisql

import (
	"fmt"
	"io/fs"
)

// Loader resolves an @include fragment name to its raw template text. Implement it to load
// fragments from anywhere (a DB table, a remote store, a cache, ...). bisql ships two
// implementations, RegistryLoader (in-memory) and FSLoader (fs.FS); there is no default —
// pass one with WithLoader when a template uses /*%! @include ... */.
type Loader interface {
	Load(name string) (string, error)
}

// LoaderFunc adapts a function to Loader.
type LoaderFunc func(name string) (string, error)

// Load implements Loader.
func (f LoaderFunc) Load(name string) (string, error) { return f(name) }

// RegistryLoader is an in-memory Loader: fragments are registered by name.
type RegistryLoader struct {
	fragments map[string]string
}

// NewRegistry creates an empty RegistryLoader.
func NewRegistry() *RegistryLoader {
	return &RegistryLoader{fragments: map[string]string{}}
}

// Register adds (or replaces) a named fragment and returns the loader for chaining.
func (r *RegistryLoader) Register(name, template string) *RegistryLoader {
	r.fragments[name] = template
	return r
}

// Load implements Loader.
func (r *RegistryLoader) Load(name string) (string, error) {
	src, ok := r.fragments[name]
	if !ok {
		return "", fmt.Errorf("bisql: unknown @include fragment %q", name)
	}
	return src, nil
}

// FSLoader loads fragments from an fs.FS (e.g. embed.FS, os.DirFS). The @include name is the
// file's path within the FS; the extension is part of the name (e.g. @include sql/active.sql).
type FSLoader struct {
	fsys fs.FS
}

// NewFSLoader creates an FSLoader over fsys.
func NewFSLoader(fsys fs.FS) *FSLoader { return &FSLoader{fsys: fsys} }

// Load implements Loader.
func (l *FSLoader) Load(name string) (string, error) {
	b, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return "", fmt.Errorf("bisql: @include %q: %w", name, err)
	}
	return string(b), nil
}
