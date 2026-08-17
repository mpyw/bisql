package render

import "reflect"

// asIterable reports whether v is a slice or array (but not a string or []byte, which bind
// as scalars) and, if so, returns its elements as []any. Used for IN-list expansion and
// for-loops.
func asIterable(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if _, ok := v.([]byte); ok {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	default:
		return nil, false
	}
}
