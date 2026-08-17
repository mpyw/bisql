package preprocess_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/preprocess"
)

func res(m map[string]string) preprocess.Resolver {
	return func(name string) (string, error) {
		s, ok := m[name]
		if !ok {
			return "", errNotFound(name)
		}
		return s, nil
	}
}

type errNotFound string

func (e errNotFound) Error() string { return "unknown fragment " + string(e) }

func TestExpand_Basic(t *testing.T) {
	got, err := preprocess.Expand("a /*%! @include frag */ b", res(map[string]string{"frag": "X"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "a X b" {
		t.Errorf("got %q", got)
	}
}

func TestExpand_Recursive(t *testing.T) {
	got, err := preprocess.Expand("/*%! @include a */", res(map[string]string{
		"a": "x /*%! @include b */",
		"b": "y /*%! @include c */",
		"c": "z",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "x y z" {
		t.Errorf("got %q", got)
	}
}

func TestExpand_Cycle(t *testing.T) {
	_, err := preprocess.Expand("/*%! @include a */", res(map[string]string{
		"a": "/*%! @include b */",
		"b": "/*%! @include a */",
	}))
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("expected cyclic error, got %v", err)
	}
}

func TestExpand_Unknown(t *testing.T) {
	if _, err := preprocess.Expand("/*%! @include nope */", res(nil)); err == nil {
		t.Error("expected unknown-fragment error")
	}
}

// @include is ignored inside string literals and quoted identifiers (not expanded).
func TestExpand_QuoteAware(t *testing.T) {
	for _, src := range []string{
		"'/*%! @include frag */'",
		"\"/*%! @include frag */\"",
		"`/*%! @include frag */`",
	} {
		got, err := preprocess.Expand(src, res(map[string]string{"frag": "BAD"}))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got != src {
			t.Errorf("%q expanded to %q (should be untouched)", src, got)
		}
	}
}

// A plain /*%! ... */ parser comment (not @include) and ordinary comments pass through.
func TestExpand_NonIncludeUntouched(t *testing.T) {
	for _, src := range []string{
		"/*%! just a note */",
		"/** block */",
		"-- line @include frag",
		"no directives here",
	} {
		got, err := preprocess.Expand(src, res(map[string]string{"frag": "BAD"}))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got != src {
			t.Errorf("%q changed to %q", src, got)
		}
	}
}

// H1: a doubled-quote (”) inside a string literal is an escaped quote, not the string's
// close, so the scanner keeps skipping until the real close — and the directive that follows
// the string still expands.
func TestExpand_DoubledQuoteEscape(t *testing.T) {
	got, err := preprocess.Expand("'can''t' /*%! @include f */", res(map[string]string{"f": "X"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "'can''t' X" {
		t.Errorf("got %q, want %q", got, "'can''t' X")
	}
}

// H2: an unterminated string literal consumes to the end of the input, so a directive inside
// the still-open string is never seen — the input is returned verbatim and the resolver is
// never called.
func TestExpand_UnterminatedString(t *testing.T) {
	src := "SELECT 'oops /*%! @include f */"
	called := false
	resolve := func(name string) (string, error) {
		called = true
		return "X", nil
	}
	got, err := preprocess.Expand(src, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("got %q, want verbatim %q", got, src)
	}
	if called {
		t.Error("resolver must not be called for a directive inside an open string")
	}
}

// H3: an unterminated block comment is left as-is (the lexer reports it later), so Expand
// returns the input verbatim with no error.
func TestExpand_UnterminatedBlockComment(t *testing.T) {
	src := "SELECT 1 /* dangling"
	got, err := preprocess.Expand(src, res(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("got %q, want verbatim %q", got, src)
	}
}

func TestExpand_NameErrors(t *testing.T) {
	for _, src := range []string{
		"/*%! @include */",     // missing name
		"/*%! @include a b */", // multi-token name
	} {
		if _, err := preprocess.Expand(src, res(map[string]string{"a": "x"})); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
	// @includes (not the keyword) is a plain parser comment
	got, err := preprocess.Expand("/*%! @includes x */", res(nil))
	if err != nil || got != "/*%! @includes x */" {
		t.Errorf("got %q err=%v", got, err)
	}
}
