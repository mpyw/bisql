package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes a set of {relative path: content} files under dir.
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

func TestRun_ExpandsFile(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"employees/search.sql":  "select emp_no from employees where 1 = 1\n/*%! @include employees/_active.sql */",
		"employees/_active.sql": "/*%if activeOnly*/and retired = /*zero*/0/*%end*/",
	})

	var out, errBuf strings.Builder
	err := run([]string{"-root", dir, "employees/search.sql"}, nil, &out, &errBuf)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errBuf.String())
	}
	want := "select emp_no from employees where 1 = 1\n/*%if activeOnly*/and retired = /*zero*/0/*%end*/"
	if out.String() != want {
		t.Errorf("output:\n got: %q\nwant: %q", out.String(), want)
	}
}

func TestRun_Stdin(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"frag.sql": "and 1 = 1"})

	in := strings.NewReader("where 1 = 1 /*%! @include frag.sql */")
	var out, errBuf strings.Builder
	if err := run([]string{"-root", dir, "-"}, in, &out, &errBuf); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errBuf.String())
	}
	if got, want := out.String(), "where 1 = 1 and 1 = 1"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

func TestRun_OutputFile(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"t.sql": "select 1"})
	outPath := filepath.Join(dir, "out.sql")

	var out, errBuf strings.Builder
	if err := run([]string{"-root", dir, "-o", outPath, "t.sql"}, nil, &out, &errBuf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty with -o, got %q", out.String())
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "select 1" {
		t.Errorf("file content %q", b)
	}
}

func TestRun_Errors(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"has_include.sql": "/*%! @include missing.sql */",
	})

	cases := []struct {
		name string
		args []string
	}{
		{"no template arg", []string{"-root", dir}},
		{"two template args", []string{"-root", dir, "a.sql", "b.sql"}},
		{"missing template file", []string{"-root", dir, "nope.sql"}},
		{"missing include fragment", []string{"-root", dir, "has_include.sql"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errBuf strings.Builder
			if err := run(c.args, nil, &out, &errBuf); err == nil {
				t.Errorf("expected an error, got none (stdout: %q)", out.String())
			}
		})
	}
}

func TestRun_HelpIsErrHelp(t *testing.T) {
	var out, errBuf strings.Builder
	err := run([]string{"-h"}, nil, &out, &errBuf)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("want flag.ErrHelp, got %v", err)
	}
}
