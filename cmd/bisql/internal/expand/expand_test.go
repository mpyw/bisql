package expand_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpyw/bisql/cmd/bisql/internal/expand"
)

// writeTree writes {relative path: content} files under dir.
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

const fragActive = "/*%if activeOnly*/and retired = /*zero*/0/*%end*/"

func TestRun_ResolvesInclude(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"employees/_active.sql": fragActive})

	in := strings.NewReader("select emp_no from employees where 1 = 1\n/*%! @include employees/_active.sql */")
	var out strings.Builder
	if err := expand.Run(root, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "select emp_no from employees where 1 = 1\n" + fragActive
	if out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

func TestRun_NoInclude(t *testing.T) {
	var out strings.Builder
	if err := expand.Run(t.TempDir(), strings.NewReader("select 1"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "select 1" {
		t.Errorf("out = %q", out.String())
	}
}

func TestRun_UnresolvedIncludeErrors(t *testing.T) {
	var out strings.Builder
	err := expand.Run(t.TempDir(), strings.NewReader("x /*%! @include missing.sql */"), &out)
	if err == nil {
		t.Errorf("expected an error for an unresolved include (out=%q)", out.String())
	}
}
