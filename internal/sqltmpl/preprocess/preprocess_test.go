package preprocess

import (
	"strings"
	"testing"
)

func res(m map[string]string) Resolver {
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
	got, err := Expand("a /*%! @include frag */ b", res(map[string]string{"frag": "X"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "a X b" {
		t.Errorf("got %q", got)
	}
}

func TestExpand_Recursive(t *testing.T) {
	got, err := Expand("/*%! @include a */", res(map[string]string{
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
	_, err := Expand("/*%! @include a */", res(map[string]string{
		"a": "/*%! @include b */",
		"b": "/*%! @include a */",
	}))
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("expected cyclic error, got %v", err)
	}
}

func TestExpand_Unknown(t *testing.T) {
	if _, err := Expand("/*%! @include nope */", res(nil)); err == nil {
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
		got, err := Expand(src, res(map[string]string{"frag": "BAD"}))
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
		got, err := Expand(src, res(map[string]string{"frag": "BAD"}))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got != src {
			t.Errorf("%q changed to %q", src, got)
		}
	}
}

func TestExpand_NameErrors(t *testing.T) {
	for _, src := range []string{
		"/*%! @include */",     // missing name
		"/*%! @include a b */", // multi-token name
	} {
		if _, err := Expand(src, res(map[string]string{"a": "x"})); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
	// @includes (not the keyword) is a plain parser comment
	got, err := Expand("/*%! @includes x */", res(nil))
	if err != nil || got != "/*%! @includes x */" {
		t.Errorf("got %q err=%v", got, err)
	}
}
