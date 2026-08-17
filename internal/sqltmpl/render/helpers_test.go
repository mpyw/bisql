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
