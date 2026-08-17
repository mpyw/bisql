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
	var out strings.Builder
	if err := runExpand(expandOptions{root: root}, in, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if want := "x " + fragActive; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

func TestFilter_FileToStdout(t *testing.T) {
	root, tmpl := fixture(t)
	var out strings.Builder
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.String() != expandSearch {
		t.Errorf("out = %q, want %q", out.String(), expandSearch)
	}
}

func TestFilter_OutputFileCreatesParents(t *testing.T) {
	root, tmpl := fixture(t)
	outPath := filepath.Join(root, "gen", "search.sql")
	var out strings.Builder
	if err := runExpand(expandOptions{root: root, inputs: []string{tmpl}, output: outPath}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty with -o, got %q", out.String())
	}
	if got := readFile(t, outPath); got != expandSearch {
		t.Errorf("file = %q", got)
	}
}

// --- tree mode ---

func TestTree_MirrorsRoot(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{"employees/list.sql": "select 1 /*%! @include employees/_active.sql */"})
	outDir := filepath.Join(t.TempDir(), "gen")

	var out strings.Builder
	if err := runExpand(expandOptions{root: root, outDir: outDir}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
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

func TestTree_InputGlob(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{
		"employees/list.sql": "select 1 /*%! @include employees/_active.sql */",
		"depts/all.sql":      "select 2",
	})
	outDir := filepath.Join(t.TempDir(), "gen")

	var out strings.Builder
	// Select only the employees directory; depts must be skipped.
	if err := runExpand(expandOptions{root: root, outDir: outDir, outName: "{{.Path}}", inputs: []string{"employees/*.sql"}}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "employees/search.sql")); err != nil {
		t.Errorf("employees/search.sql should be emitted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "depts/all.sql")); err == nil {
		t.Error("depts/all.sql should not be emitted (not matched)")
	}
}

func TestTree_OutName(t *testing.T) {
	root, _ := fixture(t)
	outDir := filepath.Join(t.TempDir(), "gen")

	var out strings.Builder
	// Keep the tree, append .gen before the extension.
	if err := runExpand(expandOptions{root: root, outDir: outDir, outName: "{{.Dir}}/{{.Name}}.gen.sql"}, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	if got := readFile(t, filepath.Join(outDir, "employees/search.gen.sql")); got != expandSearch {
		t.Errorf("renamed output = %q", got)
	}
}

func TestTree_OutNameCollision(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{"other/search.sql": "select 9"})
	outDir := filepath.Join(t.TempDir(), "gen")

	var out strings.Builder
	// Flattening to the base name collides: employees/search.sql and other/search.sql.
	err := runExpand(expandOptions{root: root, outDir: outDir, outName: "{{.Base}}"}, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Errorf("want a collision error, got %v", err)
	}
}

func TestTree_Exclude(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{
		"employees/list.sql":  "select 1 /*%! @include employees/_active.sql */",
		"partials/shared.sql": "and 1 = 1",
	})
	outDir := filepath.Join(t.TempDir(), "gen")

	var out strings.Builder
	// Exclude fragments by underscore base name (slashless -> base at any depth) and a whole
	// directory (slash -> full path).
	opts := expandOptions{root: root, outDir: outDir, exclude: []string{"_*.sql", "partials/**"}}
	if err := runExpand(opts, nil, &out); err != nil {
		t.Fatalf("runExpand: %v", err)
	}
	// Entry templates are emitted; the excluded fragment and directory are not.
	mustExist := []string{"employees/search.sql", "employees/list.sql"}
	mustNotExist := []string{"employees/_active.sql", "partials/shared.sql"}
	for _, rel := range mustExist {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Errorf("expected %s to be emitted: %v", rel, err)
		}
	}
	for _, rel := range mustNotExist {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err == nil {
			t.Errorf("expected %s to be excluded from the output", rel)
		}
	}
	// The excluded fragment is still resolvable: list.sql's include expanded.
	if got := readFile(t, filepath.Join(outDir, "employees/list.sql")); got != "select 1 "+fragActive {
		t.Errorf("list.sql did not resolve its excluded include: %q", got)
	}
}

// --- errors ---

func TestExpand_Errors(t *testing.T) {
	root, _ := fixture(t)
	writeTree(t, root, map[string]string{"broken.sql": "/*%! @include nope.sql */"})

	cases := []struct {
		name string
		opts expandOptions
	}{
		{"o and out-dir", expandOptions{root: root, output: "a", outDir: "b"}},
		{"filter two inputs", expandOptions{root: root, inputs: []string{"a.sql", "b.sql"}}},
		{"tree glob matches nothing", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen"), inputs: []string{"zzz/*.sql"}}},
		{"missing input file", expandOptions{root: root, inputs: []string{filepath.Join(root, "nope.sql")}}},
		{"missing include (filter)", expandOptions{root: root, inputs: []string{filepath.Join(root, "broken.sql")}}},
		{"missing include (tree)", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen")}},
		{"invalid exclude pattern", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen"), exclude: []string{"[bad"}}},
		{"invalid out-name", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen"), outName: "{{.Nope"}},
		{"escaping out-name", expandOptions{root: root, outDir: filepath.Join(t.TempDir(), "gen"), outName: "../{{.Base}}"}},
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
