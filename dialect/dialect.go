// Package dialect abstracts per-RDBMS placeholder generation and literal formatting
// (for the /*^ */ literal directive). bisql abstracts only this; it does not handle SQL
// dialects themselves.
package dialect

import (
	"fmt"
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

var (
	// MySQL uses ? placeholders.
	MySQL = Dialect{name: "mysql", placeholder: func(int, string) string { return "?" }}
	// PostgreSQL uses $1, $2, ... placeholders.
	PostgreSQL = Dialect{name: "postgresql", placeholder: func(i int, _ string) string { return "$" + strconv.Itoa(i) }}
	// Oracle uses :1, :2, ... placeholders.
	Oracle = Dialect{name: "oracle", placeholder: func(i int, _ string) string { return ":" + strconv.Itoa(i) }}
	// SQLServer uses @p1, @p2, ... placeholders.
	SQLServer = Dialect{name: "sqlserver", placeholder: func(i int, _ string) string { return "@p" + strconv.Itoa(i) }}
)
