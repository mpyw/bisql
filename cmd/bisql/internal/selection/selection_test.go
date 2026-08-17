package selection

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTree(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, name := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("select 1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSelectInputs covers every input shape: one file, one glob, many files, many globs, a
// file+glob mix, overlapping patterns (which must de-duplicate), and no globs (= all).
func TestSelectInputs(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "x/a.sql", "x/b.sql", "y/c.sql")
	cases := []struct {
		name  string
		globs []string
		want  []string
	}{
		{"single file", []string{"x/a.sql"}, []string{"x/a.sql"}},
		{"single glob", []string{"x/*.sql"}, []string{"x/a.sql", "x/b.sql"}},
		{"multiple files", []string{"x/a.sql", "y/c.sql"}, []string{"x/a.sql", "y/c.sql"}},
		{"multiple globs", []string{"x/*.sql", "y/*.sql"}, []string{"x/a.sql", "x/b.sql", "y/c.sql"}},
		{"file and glob", []string{"x/a.sql", "y/*.sql"}, []string{"x/a.sql", "y/c.sql"}},
		{"overlap dedups", []string{"x/*.sql", "x/a.sql"}, []string{"x/a.sql", "x/b.sql"}},
		{"none means all", nil, []string{"x/a.sql", "x/b.sql", "y/c.sql"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SelectInputs(root, c.globs)
			if err != nil {
				t.Fatalf("SelectInputs: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("selected %v, want %v", got, c.want)
			}
		})
	}
}

func TestSelectInputs_Errors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.sql")
	if _, err := SelectInputs(root, []string{"nope/*.sql"}); err == nil {
		t.Error("a glob matching nothing should error")
	}
	if _, err := SelectInputs(t.TempDir(), nil); err == nil {
		t.Error("an empty tree should error")
	}
	if _, err := SelectInputs(root, []string{"[bad"}); err == nil {
		t.Error("an invalid glob should error")
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		rel, pat string
		want     bool
	}{
		{"employees/_active.sql", "_*.sql", true},      // slashless -> base at any depth
		{"employees/search.sql", "_*.sql", false},      // base does not match
		{"partials/shared.sql", "partials/**", true},   // slash -> full path, ** spans dirs
		{"employees/search.sql", "partials/**", false}, // different directory
		{"employees/search.sql", "employees/*.sql", true},
		{"employees/sub/x.sql", "employees/*.sql", false}, // * does not cross a slash
	}
	for _, c := range cases {
		got, err := Match(c.rel, []string{c.pat})
		if err != nil {
			t.Fatalf("Match(%q,%q): %v", c.rel, c.pat, err)
		}
		if got != c.want {
			t.Errorf("Match(%q,%q) = %v, want %v", c.rel, c.pat, got, c.want)
		}
	}
}
