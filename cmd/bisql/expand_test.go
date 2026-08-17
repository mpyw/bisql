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

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"employees/search.sql":  tmplSearch,
		"employees/_active.sql": fragActive,
	})
	return dir
}

func TestExpand_Stdin(t *testing.T) {
	dir := fixture(t)
	in := strings.NewReader("x /*%! @include employees/_active.sql */")
	var out, errBuf strings.Builder
	err := runExpand(expandOptions{root: dir}, in, &out, &errBuf)
	if err != nil {
		t.Fatalf("runExpand: %v (%s)", err, errBuf.String())
	}
	if want := "x " + fragActive; out.String() != want {
		t.Errorf("stdin out = %q, want %q", out.String(), want)
	}
}

func TestExpand_SingleFileToStdout(t *testing.T) {
	dir := fixture(t)
	var out, errBuf strings.Builder
	err := runExpand(expandOptions{root: dir, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf)
	if err != nil {
		t.Fatalf("runExpand: %v (%s)", err, errBuf.String())
	}
	if out.String() != expandSearch {
		t.Errorf("out = %q, want %q", out.String(), expandSearch)
	}
}

func TestExpand_OutputFile(t *testing.T) {
	dir := fixture(t)
	outPath := filepath.Join(dir, "gen.sql")
	var out, errBuf strings.Builder
	err := runExpand(expandOptions{root: dir, output: outPath, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf)
	if err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty with -o, got %q", out.String())
	}
	if got := readFile(t, outPath); got != expandSearch {
		t.Errorf("file = %q", got)
	}
}

func TestExpand_WriteInPlace(t *testing.T) {
	dir := fixture(t)
	var out, errBuf strings.Builder
	err := runExpand(expandOptions{root: dir, write: true, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf)
	if err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "employees/search.sql")); got != expandSearch {
		t.Errorf("in-place = %q", got)
	}
}

func TestExpand_DirectoryToOutputDir(t *testing.T) {
	dir := fixture(t)
	writeTree(t, dir, map[string]string{"employees/list.sql": "select 1 /*%! @include employees/_active.sql */"})
	outDir := filepath.Join(dir, "gen")
	var out, errBuf strings.Builder
	err := runExpand(expandOptions{root: dir, output: outDir, inputs: []string{"."}}, nil, &out, &errBuf)
	if err != nil {
		t.Fatalf("runExpand: %v (%s)", err, errBuf.String())
	}
	// The fragment, the search, and the list templates are all mirrored.
	for _, rel := range []string{"employees/search.sql", "employees/list.sql", "employees/_active.sql"} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Errorf("missing mirrored output %s: %v", rel, err)
		}
	}
	if got := readFile(t, filepath.Join(outDir, "employees/search.sql")); got != expandSearch {
		t.Errorf("mirrored search = %q", got)
	}
}

func TestExpand_Check(t *testing.T) {
	dir := fixture(t)

	// Not yet expanded on disk -> drift -> error.
	var out, errBuf strings.Builder
	if err := runExpand(expandOptions{root: dir, check: true, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf); err == nil {
		t.Error("check should report drift on an un-expanded file")
	} else if _, ok := err.(quietExit); !ok {
		t.Errorf("check drift error = %T, want quietExit", err)
	}

	// After materializing it, check passes.
	if err := runExpand(expandOptions{root: dir, write: true, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	errBuf.Reset()
	if err := runExpand(expandOptions{root: dir, check: true, inputs: []string{"employees/search.sql"}}, nil, &out, &errBuf); err != nil {
		t.Errorf("check should pass after -w, got %v", err)
	}
}

func TestExpand_Errors(t *testing.T) {
	dir := fixture(t)
	cases := []struct {
		name string
		opts expandOptions
	}{
		{"write and check", expandOptions{root: dir, write: true, check: true, inputs: []string{"employees/search.sql"}}},
		{"stdin with write", expandOptions{root: dir, write: true}},
		{"stdin with check", expandOptions{root: dir, check: true}},
		{"mix stdin and file", expandOptions{root: dir, inputs: []string{"-", "employees/search.sql"}}},
		{"multiple inputs no dest", expandOptions{root: dir, inputs: []string{"employees/search.sql", "employees/_active.sql"}}},
		{"missing include", expandOptions{root: dir, inputs: []string{"broken.sql"}}},
		{"outside root", expandOptions{root: filepath.Join(dir, "employees"), inputs: []string{"../employees/search.sql"}}},
	}
	// broken.sql references a missing fragment.
	writeTree(t, dir, map[string]string{"broken.sql": "/*%! @include nope.sql */"})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errBuf strings.Builder
			if err := runExpand(c.opts, strings.NewReader("x"), &out, &errBuf); err == nil {
				t.Errorf("expected an error (out=%q)", out.String())
			}
		})
	}
}
