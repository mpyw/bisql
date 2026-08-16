// Package dialect abstracts per-RDBMS placeholder generation (and, later, literal
// formatting). bisql abstracts only this; it does not handle SQL dialects themselves.
package dialect

import "strconv"

// Placeholder generates a placeholder. index is the 1-based sequential number; name is the
// bind name (empty if anonymous).
type Placeholder func(index int, name string) string

// Dialect carries a name and its placeholder generator.
type Dialect struct {
	name        string
	placeholder Placeholder
}

// Name returns the dialect name.
func (d Dialect) Name() string { return d.name }

// Placeholder returns the placeholder generator.
func (d Dialect) Placeholder() Placeholder { return d.placeholder }

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
