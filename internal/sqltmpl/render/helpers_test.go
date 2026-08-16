package render

import (
	"reflect"
	"testing"
)

func TestAsIterable(t *testing.T) {
	iter := []struct {
		name string
		in   any
		want []any
	}{
		{"slice of any", []any{1, "a", nil}, []any{1, "a", nil}},
		{"slice of int", []int{1, 2, 3}, []any{1, 2, 3}},
		{"array", [2]string{"x", "y"}, []any{"x", "y"}},
		{"empty slice", []any{}, []any{}},
	}
	for _, c := range iter {
		t.Run(c.name, func(t *testing.T) {
			got, ok := asIterable(c.in)
			if !ok || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v ok=%v", got, ok)
			}
		})
	}
	notIter := []struct {
		name string
		in   any
	}{
		{"string", "abc"}, {"[]byte", []byte{1, 2}}, {"nil", nil},
		{"map", map[string]any{"k": 1}}, {"scalar", 5},
	}
	for _, c := range notIter {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := asIterable(c.in); ok {
				t.Errorf("%#v must not be iterable", c.in)
			}
		})
	}
}

func TestMembersOf(t *testing.T) {
	type inner struct {
		Exported   int
		unexported string //nolint:unused // present to prove it is skipped
	}
	t.Run("struct", func(t *testing.T) {
		m, err := membersOf(inner{Exported: 7, unexported: "x"})
		if err != nil || !reflect.DeepEqual(m, map[string]any{"Exported": 7}) {
			t.Errorf("got %#v err=%v", m, err)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		m, err := membersOf(&inner{Exported: 9})
		if err != nil || m["Exported"] != 9 {
			t.Errorf("got %#v err=%v", m, err)
		}
	})
	t.Run("map", func(t *testing.T) {
		m, err := membersOf(map[string]any{"a": 1})
		if err != nil || !reflect.DeepEqual(m, map[string]any{"a": 1}) {
			t.Errorf("got %#v err=%v", m, err)
		}
	})
	t.Run("errors", func(t *testing.T) {
		if _, err := membersOf(map[int]any{1: "x"}); err == nil {
			t.Error("non-string-key map must error")
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
