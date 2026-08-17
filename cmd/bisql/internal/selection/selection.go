// Package selection discovers and filters the *.sql templates of a tree by glob. Globs follow
// the gitignore convention: a pattern with a slash is matched against the whole relative path
// (with ** spanning directories); a slashless pattern is matched against the base name at any
// depth.
package selection

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// SelectInputs returns the *.sql files under root (as sorted slash paths relative to root)
// that match any of the globs; with no globs, every *.sql file is selected.
func SelectInputs(root string, globs []string) ([]string, error) {
	all, err := sqlFilesUnder(root)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no .sql templates found under %s", root)
	}
	if len(globs) == 0 {
		return all, nil
	}
	var sel []string
	for _, rel := range all {
		ok, err := Match(rel, globs)
		if err != nil {
			return nil, err
		}
		if ok {
			sel = append(sel, rel)
		}
	}
	if len(sel) == 0 {
		return nil, fmt.Errorf("no .sql files matched: %s", strings.Join(globs, ", "))
	}
	return sel, nil
}

// Match reports whether the slash path rel matches any of the glob patterns.
func Match(rel string, patterns []string) (bool, error) {
	for _, p := range patterns {
		name := rel
		if !strings.Contains(p, "/") {
			name = path.Base(rel)
		}
		ok, err := doublestar.Match(p, name)
		if err != nil {
			return false, fmt.Errorf("invalid glob %q: %w", p, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// sqlFilesUnder returns the *.sql files below root as sorted slash paths relative to root.
func sqlFilesUnder(root string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".sql") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(rels)
	return rels, nil
}
