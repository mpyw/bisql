package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	tmplSearch   = "select emp_no from employees where 1 = 1\n/*%! @include employees/_active.sql */"
	fragActive   = "/*%if activeOnly*/and retired = /*zero*/0/*%end*/"
	expandSearch = "select emp_no from employees where 1 = 1\n/*%if activeOnly*/and retired = /*zero*/0/*%end*/"
)

// fixture writes a root with a template and its fragment, and returns the root and the
// template's path.
func fixture(t *testing.T) (root, tmpl string) {
	t.Helper()
	root = t.TempDir()
	writeTree(t, root, map[string]string{
		"employees/search.sql":  tmplSearch,
		"employees/_active.sql": fragActive,
	})
	return root, filepath.Join(root, "employees/search.sql")
}

func TestExpand_Stdin(t *testing.T) {
	root, _ := fixture(t)
	in := strings.NewReader("x /*%! @include employees/_active.sql */")
	var out strings.Builder
	if err := runExpand(expandOptions{root: root}, in, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if want := "x " + fragActive; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

func TestExpand_FileToStdout(t *testing.T) {
	root, tmpl := fixture(t)
	var out strings.Builder
	if err := runExpand(expandOptions{root: root, input: tmpl}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.String() != expandSearch {
		t.Errorf("out = %q, want %q", out.String(), expandSearch)
	}
}

func TestExpand_OutputFileCreatesParents(t *testing.T) {
	root, tmpl := fixture(t)
	outPath := filepath.Join(root, "gen", "search.sql") // gen/ does not exist yet
	var out strings.Builder
	if err := runExpand(expandOptions{root: root, input: tmpl, output: outPath}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty with -o, got %q", out.String())
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != expandSearch {
		t.Errorf("file = %q", b)
	}
}

func TestExpand_Check(t *testing.T) {
	root, tmpl := fixture(t)
	genPath := filepath.Join(root, "gen.sql")
	var out strings.Builder

	// Absent target -> out of date.
	if err := runExpand(expandOptions{root: root, input: tmpl, check: genPath}, nil, &out); err == nil {
		t.Error("check should fail when the target does not exist")
	}

	// Materialize, then check passes.
	if err := runExpand(expandOptions{root: root, input: tmpl, output: genPath}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := runExpand(expandOptions{root: root, input: tmpl, check: genPath}, nil, &out); err != nil {
		t.Errorf("check should pass after -o, got %v", err)
	}

	// Drift the target -> out of date.
	if err := os.WriteFile(genPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runExpand(expandOptions{root: root, input: tmpl, check: genPath}, nil, &out); err == nil {
		t.Error("check should fail on drift")
	}
}

func TestExpand_Errors(t *testing.T) {
	root, tmpl := fixture(t)
	writeTree(t, root, map[string]string{"broken.sql": "/*%! @include nope.sql */"})

	cases := []struct {
		name string
		opts expandOptions
	}{
		{"o and check", expandOptions{root: root, input: tmpl, output: "a", check: "b"}},
		{"missing input file", expandOptions{root: root, input: filepath.Join(root, "nope.sql")}},
		{"missing include", expandOptions{root: root, input: filepath.Join(root, "broken.sql")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out strings.Builder
			if err := runExpand(c.opts, strings.NewReader("x"), &out); err == nil {
				t.Errorf("expected an error (out=%q)", out.String())
			}
		})
	}
}
