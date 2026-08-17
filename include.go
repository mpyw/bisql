package bisql

import (
	"errors"
	"fmt"
	"io/fs"
)

// ErrNotFound reports that a Loader does not have the requested fragment. A Loader signals a
// missing fragment (as opposed to a genuine failure such as an I/O error) by returning an
// error e for which errors.Is(e, ErrNotFound) holds; a StackedLoader then falls through to
// the next loader. FSLoader reports a missing file with fs.ErrNotExist, which a StackedLoader
// also treats as "not found".
var ErrNotFound = errors.New("bisql: @include fragment not found")

// Loader resolves an @include fragment name to its raw template text. Implement it to load
// fragments from anywhere (a DB table, a remote store, a cache, ...). bisql ships three
// implementations, RegistryLoader (in-memory), FSLoader (fs.FS), and StackedLoader (a chain);
// there is no default — pass one with WithLoader when a template uses /*%! @include ... */.
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

// NewRegistryLoader creates an empty RegistryLoader.
func NewRegistryLoader() *RegistryLoader {
	return &RegistryLoader{fragments: map[string]string{}}
}

// Register adds (or replaces) a named fragment and returns the loader for chaining.
func (r *RegistryLoader) Register(name, template string) *RegistryLoader {
	r.fragments[name] = template
	return r
}

// Load implements Loader. An unregistered name returns an error satisfying ErrNotFound.
func (r *RegistryLoader) Load(name string) (string, error) {
	src, ok := r.fragments[name]
	if !ok {
		return "", fmt.Errorf("bisql: @include fragment %q is not registered: %w", name, ErrNotFound)
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

// Load implements Loader. A missing file returns an error satisfying fs.ErrNotExist (which a
// StackedLoader treats as "not found").
func (l *FSLoader) Load(name string) (string, error) {
	b, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return "", fmt.Errorf("bisql: @include %q: %w", name, err)
	}
	return string(b), nil
}

// StackedLoader resolves a fragment by trying its loaders in order. It falls through to the
// next loader whenever one reports the fragment is not found (errors.Is(err, ErrNotFound), or
// fs.ErrNotExist); any other error aborts the lookup immediately. If no loader has the
// fragment, Load returns an error satisfying ErrNotFound, so stacks compose.
type StackedLoader struct {
	loaders []Loader
}

// NewStackedLoader creates a StackedLoader over loaders, tried in the given order.
func NewStackedLoader(loaders ...Loader) *StackedLoader {
	return &StackedLoader{loaders: loaders}
}

// Load implements Loader.
func (s *StackedLoader) Load(name string) (string, error) {
	for _, l := range s.loaders {
		src, err := l.Load(name)
		if err == nil {
			return src, nil
		}
		if isNotFound(err) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("bisql: @include fragment %q not found in any of %d loaders: %w", name, len(s.loaders), ErrNotFound)
}

// isNotFound reports whether err signals a missing fragment (as opposed to a real failure).
func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}
