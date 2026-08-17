// Package outname turns a Go-template format string into output paths for expanded templates.
// The template names each output relative to the output directory, given the fields of the
// input path. It is used by the expand command's --out-name-format flag.
package outname

import (
	"fmt"
	"path"
	"strings"
	"text/template"
)

// Fields are the template fields for one input path.
type Fields struct {
	Path string // full relative slash path, e.g. "employees/search.sql"
	Dir  string // directory, e.g. "employees" ("." at the root)
	Base string // file name with extension, e.g. "search.sql"
	Name string // file name without the final extension, e.g. "search"
	Ext  string // final extension including the dot, e.g. ".sql"
}

// Format is a parsed --out-name-format template.
type Format struct {
	tmpl *template.Template
}

// Parse compiles the format string. An empty string yields the default, which mirrors the
// input path ({{.Path}}).
func Parse(s string) (Format, error) {
	if s == "" {
		s = "{{.Path}}"
	}
	t, err := template.New("out-name-format").Parse(s)
	if err != nil {
		return Format{}, fmt.Errorf("invalid --out-name-format template: %w", err)
	}
	return Format{tmpl: t}, nil
}

// Render evaluates the format for the input path rel and returns the cleaned output path,
// relative to the output directory. It rejects an empty result and one that escapes the
// output directory (absolute, or reaching above it with "..").
func (f Format) Render(rel string) (string, error) {
	ext := path.Ext(rel)
	base := path.Base(rel)
	var b strings.Builder
	if err := f.tmpl.Execute(&b, Fields{
		Path: rel,
		Dir:  path.Dir(rel),
		Base: base,
		Name: strings.TrimSuffix(base, ext),
		Ext:  ext,
	}); err != nil {
		return "", fmt.Errorf("--out-name-format for %s: %w", rel, err)
	}
	out := path.Clean(strings.TrimSpace(b.String()))
	if out == "" || out == "." {
		return "", fmt.Errorf("--out-name-format produced an empty path for %s", rel)
	}
	if path.IsAbs(out) || out == ".." || strings.HasPrefix(out, "../") {
		return "", fmt.Errorf("--out-name-format produced a path outside --out-dir for %s: %q", rel, out)
	}
	return out, nil
}
