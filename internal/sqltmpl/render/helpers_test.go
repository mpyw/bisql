package render

import (
	"reflect"
	"testing"
)

func TestAsIterable(t *testing.T) {
	// Only slices/arrays are iterable (expandable into an IN list). A string is a single
	// scalar bind, never exploded into characters; []byte is a single blob; maps and other
	// scalars are not iterable. want is only meaningful when the value is iterable.
	iterable := []struct {
		name string
		in   any
		want []any
	}{
		{"slice of any", []any{1, "a", nil}, []any{1, "a", nil}},
		{"slice of int", []int{1, 2, 3}, []any{1, 2, 3}},
		{"array", [2]string{"x", "y"}, []any{"x", "y"}},
		{"empty slice", []any{}, []any{}},
	}
	for _, c := range iterable {
		t.Run(c.name, func(t *testing.T) {
			got, ok := asIterable(c.in)
			if !ok {
				t.Fatalf("expected iterable")
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}

	notIterable := []struct {
		name string
		in   any
	}{
		{"string is a scalar", "abc"},
		{"[]byte is a scalar blob", []byte{1, 2}},
		{"nil", nil},
		{"map", map[string]any{"k": 1}},
		{"scalar int", 5},
	}
	for _, c := range notIterable {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := asIterable(c.in); ok {
				t.Errorf("%#v must not be iterable", c.in)
			}
		})
	}
}

func TestToStr(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"", ""},
		{"hello", "hello"},
		{42, "42"},
		{true, "true"},
		{3.5, "3.5"},
	}
	for _, c := range cases {
		if got := toStr(c.in); got != c.want {
			t.Errorf("toStr(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMembersOf(t *testing.T) {
	type inner struct {
		Exported   int
		unexported string //nolint:unused // present to prove it is skipped
	}

	t.Run("struct exported fields only", func(t *testing.T) {
		m, err := membersOf(inner{Exported: 7, unexported: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(m, map[string]any{"Exported": 7}) {
			t.Errorf("got %#v", m)
		}
	})

	t.Run("pointer to struct", func(t *testing.T) {
		m, err := membersOf(&inner{Exported: 9})
		if err != nil {
			t.Fatal(err)
		}
		if m["Exported"] != 9 {
			t.Errorf("got %#v", m)
		}
	})

	t.Run("string-keyed map", func(t *testing.T) {
		m, err := membersOf(map[string]any{"a": 1, "b": 2})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(m, map[string]any{"a": 1, "b": 2}) {
			t.Errorf("got %#v", m)
		}
	})

	t.Run("errors", func(t *testing.T) {
		if _, err := membersOf(map[int]any{1: "x"}); err == nil {
			t.Error("non-string-keyed map must error")
		}
		if _, err := membersOf(42); err == nil {
			t.Error("scalar must error")
		}
		var p *inner
		if _, err := membersOf(p); err == nil {
			t.Error("nil pointer must error")
		}
	})
}
