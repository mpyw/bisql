package expand_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpyw/bisql/cmd/bisql/internal/expand"
)

// errReader is an io.Reader that always fails with a non-EOF error, to exercise Run's
// "reading stdin" error path.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

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

const fragActive = "/*%if activeOnly*/and status = /*status*/'active'/*%end*/"

func TestRun_ResolvesInclude(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"users/_active.sql": fragActive})

	in := strings.NewReader("select id from users where 1 = 1\n/*%! @include users/_active.sql */")
	var out strings.Builder
	if err := expand.Run(root, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "select id from users where 1 = 1\n" + fragActive
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
		t.Fatalf("expected an error for an unresolved include (out=%q)", out.String())
	}
	// FSLoader wraps the underlying fs.ErrNotExist for a missing fragment.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
	// Run writes to stdout only after a successful expand, so nothing must be emitted here.
	if out.String() != "" {
		t.Errorf("out = %q, want empty on the error path", out.String())
	}
}

// TestRun_ResolvesRecursiveIncludes verifies that Run inlines includes transitively:
// a.sql -> b.sql -> c.sql, with all three fragments present in the final output.
func TestRun_ResolvesRecursiveIncludes(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"b.sql": "B-body\n/*%! @include c.sql */",
		"c.sql": "C-body",
	})

	in := strings.NewReader("A-body\n/*%! @include b.sql */")
	var out strings.Builder
	if err := expand.Run(root, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	for _, want := range []string{"A-body", "B-body", "C-body"} {
		if !strings.Contains(got, want) {
			t.Errorf("out = %q, want it to contain %q", got, want)
		}
	}
}

// TestRun_CyclicIncludeErrors verifies that a cyclic @include chain (A -> B -> A) is
// rejected rather than expanded forever.
func TestRun_CyclicIncludeErrors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.sql": "/*%! @include b.sql */",
		"b.sql": "/*%! @include a.sql */",
	})

	var out strings.Builder
	err := expand.Run(root, strings.NewReader("/*%! @include a.sql */"), &out)
	if err == nil {
		t.Fatalf("expected an error for a cyclic include (out=%q)", out.String())
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("err = %v, want it to mention %q", err, "cyclic")
	}
}

// TestRun_StdinReadError verifies that a failing stdin reader surfaces as a "reading stdin"
// error rather than being silently treated as an empty template.
func TestRun_StdinReadError(t *testing.T) {
	var out strings.Builder
	err := expand.Run(t.TempDir(), errReader{err: errors.New("boom")}, &out)
	if err == nil {
		t.Fatalf("expected an error for a failing stdin reader")
	}
	if !strings.Contains(err.Error(), "reading stdin") {
		t.Errorf("err = %v, want it to mention %q", err, "reading stdin")
	}
}

// TestRun_IncludeInsideStringLiteralNotExpanded verifies the preprocessor is string/comment
// aware: an @include-looking directive inside a '...' literal is passed through unchanged,
// even though no such fragment exists on disk.
func TestRun_IncludeInsideStringLiteralNotExpanded(t *testing.T) {
	src := "select '/*%! @include x.sql */'"
	var out strings.Builder
	if err := expand.Run(t.TempDir(), strings.NewReader(src), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != src {
		t.Errorf("out = %q, want unchanged %q", out.String(), src)
	}
}

var _ io.Reader = errReader{}
