// Package dialect abstracts per-RDBMS placeholder generation and literal formatting
// (for the /*^ */ literal directive). bisql abstracts only this; it does not handle SQL
// dialects themselves.
package dialect

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Placeholder generates a placeholder. index is the 1-based sequential number; name is the
// bind name (empty if anonymous).
type Placeholder func(index int, name string) string

// Literal formats a Go value as an inline SQL literal, for the /*^ */ directive.
type Literal func(v any) (string, error)

// Dialect carries a name, its placeholder generator, and its literal formatter.
type Dialect struct {
	name        string
	placeholder Placeholder
	literal     Literal
}

// Name returns the dialect name.
func (d Dialect) Name() string { return d.name }

// Placeholder returns the placeholder generator.
func (d Dialect) Placeholder() Placeholder { return d.placeholder }

// Literal returns the literal formatter, falling back to FormatLiteral when unset.
func (d Dialect) Literal() Literal {
	if d.literal != nil {
		return d.literal
	}
	return FormatLiteral
}

// FormatLiteral is the default literal formatter: nil -> null, strings are single-quoted
// with ” escaping, booleans and numbers render bare. It is deliberately conservative;
// dialects with different quoting rules can supply their own.
func FormatLiteral(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", x), nil
	default:
		return "", fmt.Errorf("bisql/dialect: cannot format %T as a SQL literal", v)
	}
}

// formatPostgresLiteral extends FormatLiteral with PostgreSQL array literals: a slice or array
// (other than []byte, which is a scalar bytea) renders as '{...}', the canonical array input
// syntax. This makes the values-embedded review form of an array bind — e.g.
// `= ANY($1::int[])` — valid PostgreSQL (`= ANY('{10,20,30}'::int[])`) rather than a Go %v dump.
// It is still review-only output; do not execute it.
func formatPostgresLiteral(v any) (string, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if _, isBytes := v.([]byte); isBytes {
			return FormatLiteral(v)
		}
		body, err := postgresArrayBody(rv)
		if err != nil {
			return "", err
		}
		// The whole array literal is a single-quoted string, so escape any single quotes.
		return "'" + strings.ReplaceAll(body, "'", "''") + "'", nil
	default:
		return FormatLiteral(v)
	}
}

// postgresArrayBody renders the {e1,e2,...} body of a (possibly nested) array, without the
// surrounding single quotes.
func postgresArrayBody(rv reflect.Value) (string, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < rv.Len(); i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		elem := rv.Index(i).Interface()
		ev := reflect.ValueOf(elem)
		if k := ev.Kind(); k == reflect.Slice || k == reflect.Array {
			if _, isBytes := elem.([]byte); !isBytes {
				inner, err := postgresArrayBody(ev)
				if err != nil {
					return "", err
				}
				b.WriteString(inner)
				continue
			}
		}
		s, err := postgresArrayElem(elem)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// postgresArrayElem renders one scalar array element in PostgreSQL array text format: NULL for
// nil, a double-quoted (backslash-escaped) string for strings, and the bare FormatLiteral form
// for numbers and booleans.
func postgresArrayElem(v any) (string, error) {
	if v == nil {
		return "NULL", nil
	}
	if s, ok := v.(string); ok {
		esc := strings.ReplaceAll(s, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		return `"` + esc + `"`, nil
	}
	return FormatLiteral(v)
}

var (
	// MySQL uses ? placeholders.
	MySQL = Dialect{name: "mysql", placeholder: func(int, string) string { return "?" }}
	// PostgreSQL uses $1, $2, ... placeholders and renders array literals as '{...}'.
	PostgreSQL = Dialect{
		name:        "postgresql",
		placeholder: func(i int, _ string) string { return "$" + strconv.Itoa(i) },
		literal:     formatPostgresLiteral,
	}
	// Oracle uses :1, :2, ... placeholders.
	Oracle = Dialect{name: "oracle", placeholder: func(i int, _ string) string { return ":" + strconv.Itoa(i) }}
	// SQLServer uses @p1, @p2, ... placeholders.
	SQLServer = Dialect{name: "sqlserver", placeholder: func(i int, _ string) string { return "@p" + strconv.Itoa(i) }}
)
