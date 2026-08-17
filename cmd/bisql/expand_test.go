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

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	tmplSearch   = "select emp_no from employees where 1 = 1\n/*%! @include employees/_active.sql */"
	fragActive   = "/*%if activeOnly*/and retired = /*zero*/0/*%end*/"
	expandSearch = "select emp_no from employees where 1 = 1\n/*%if activeOnly*/and retired = /*zero*/0/*%end*/"
)

func fixture(t *testing.T) (root, tmpl string) {
	t.Helper()
	root = t.TempDir()
	writeTree(t, root, map[string]string{
		"employees/search.sql":  tmplSearch,
		"employees/_active.sql": fragActive,
	})
	return root, filepath.Join(root, "employees/search.sql")
}

// --- filter mode ---

func TestFilter_Stdin(t *testing.T) {
	root, _ := fixture(t)
	in := strings.NewReader("x /*%! @include employees/_active.sql */")
	var out, errBuf strings.Builder
	if err := runExpand(expandOptions{root: root}, in, &out, &errBuf); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if want := "x " + fragActive; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

func TestFilter_FileToStdout(t *testing.T) {
	root, tmpl := fixture(t)
	var out, errBuf strings.Builder
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}}, nil, &out, &errBuf); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.String() != expandSearch {
		t.Errorf("out = %q, want %q", out.String(), expandSearch)
	}
}

func TestFilter_OutputFileCreatesParents(t *testing.T) {
	root, tmpl := fixture(t)
	outPath := filepath.Join(root, "gen", "search.sql")
	var out, errBuf strings.Builder
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}, output: outPath}, nil, &out, &errBuf); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty with -o, got %q", out.String())
	}
	if got := readFile(t, outPath); got != expandSearch {
		t.Errorf("file = %q", got)
	}
}

func TestFilter_Check(t *testing.T) {
	root, tmpl := fixture(t)
	genPath := filepath.Join(root, "gen.sql")
	var out, errBuf strings.Builder

	// Absent target -> out of date.
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}, output: genPath, check: true}, nil, &out, &errBuf); err == nil {
		t.Error("check should fail when the target does not exist")
	}
	// Materialize, then check passes.
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}, output: genPath}, nil, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}, output: genPath, check: true}, nil, &out, &errBuf); err != nil {
		t.Errorf("check should pass after -o, got %v", err)
	}
}

// --- tree mode ---

func TestTree_MirrorsRoot(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{"employees/list.sql": "select 1 /*%! @include employees/_active.sql */"})
	outDir := filepath.Join(t.TempDir(), "gen")

	var out, errBuf strings.Builder
	if err := runExpand(expandOptions{root: root, outDir: outDir}, nil, &out, &errBuf); err != nil {
		t.Fatalf("runExpand: %v (%s)", err, errBuf.String())
	}
	// Every *.sql is mirrored, including the fragment (expanded to itself).
	if got := readFile(t, filepath.Join(outDir, "employees/search.sql")); got != expandSearch {
		t.Errorf("mirrored search = %q", got)
	}
	if got := readFile(t, filepath.Join(outDir, "employees/list.sql")); got != "select 1 "+fragActive {
		t.Errorf("mirrored list = %q", got)
	}
	if got := readFile(t, filepath.Join(outDir, "employees/_active.sql")); got != fragActive {
		t.Errorf("mirrored fragment = %q", got)
	}
}

func TestTree_Check(t *testing.T) {
	root, _ := fixture(t)
	outDir := filepath.Join(t.TempDir(), "gen")
	var out, errBuf strings.Builder

	// Nothing generated yet -> drift.
	if err := runExpand(expandOptions{root: root, outDir: outDir, check: true}, nil, &out, &errBuf); err == nil {
		t.Error("check should fail before the tree is generated")
	}
	// Generate, then check passes.
	if err := runExpand(expandOptions{root: root, outDir: outDir}, nil, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	if err := runExpand(expandOptions{root: root, outDir: outDir, check: true}, nil, &out, &errBuf); err != nil {
		t.Errorf("check should pass after generation, got %v", err)
	}
}

// --- errors ---

func TestExpand_Errors(t *testing.T) {
	root, tmpl := fixture(t)
	writeTree(t, root, map[string]string{"broken.sql": "/*%! @include nope.sql */"})

	cases := []struct {
		name string
		opts expandOptions
	}{
		{"o and out-dir", expandOptions{root: root, output: "a", outDir: "b"}},
		{"filter two inputs", expandOptions{root: root, inputs: []string{"a.sql", "b.sql"}}},
		{"filter check without -o", expandOptions{root: root, inputs: []string{tmpl}, check: true}},
		{"tree with file arg", expandOptions{root: root, inputs: []string{tmpl}, outDir: "gen"}},
		{"missing input file", expandOptions{root: root, inputs: []string{filepath.Join(root, "nope.sql")}}},
		{"missing include", expandOptions{root: root, inputs: []string{filepath.Join(root, "broken.sql")}}},
		{"tree missing include", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errBuf strings.Builder
			if err := runExpand(c.opts, strings.NewReader("x"), &out, &errBuf); err == nil {
				t.Errorf("expected an error (out=%q)", out.String())
			}
		})
	}
}
