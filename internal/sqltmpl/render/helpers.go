package render

import (
	"fmt"
	"reflect"
)

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

// toStr renders v for an embedded value directive: nil becomes the empty string, otherwise
// the default Go formatting.
func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// membersOf extracts the properties of v (exported struct fields or string-keyed map
// entries) for a with block.
func membersOf(v any) (map[string]any, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("nil pointer has no properties")
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key type %s is not a string", rv.Type().Key())
		}
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[k.String()] = rv.MapIndex(k).Interface()
		}
		return out, nil
	case reflect.Struct:
		t := rv.Type()
		out := make(map[string]any, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.IsExported() {
				out[f.Name] = rv.Field(i).Interface()
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%T is neither a struct nor a map", v)
	}
}
