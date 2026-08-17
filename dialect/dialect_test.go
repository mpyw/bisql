package dialect_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql/dialect"
)

func TestNamesAndPlaceholders(t *testing.T) {
	cases := []struct {
		d      dialect.Dialect
		name   string
		at1    string // placeholder for index 1
		at2    string // placeholder for index 2
		ignore bool   // whether index is ignored (MySQL: always "?")
	}{
		{dialect.MySQL, "mysql", "?", "?", true},
		{dialect.PostgreSQL, "postgresql", "$1", "$2", false},
		{dialect.Oracle, "oracle", ":1", ":2", false},
		{dialect.SQLServer, "sqlserver", "@p1", "@p2", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.d.Name(); got != c.name {
				t.Errorf("Name() = %q, want %q", got, c.name)
			}
			ph := c.d.Placeholder()
			if got := ph(1, ""); got != c.at1 {
				t.Errorf("placeholder(1) = %q, want %q", got, c.at1)
			}
			if got := ph(2, ""); got != c.at2 {
				t.Errorf("placeholder(2) = %q, want %q", got, c.at2)
			}
			if c.ignore && ph(1, "") != ph(99, "") {
				t.Errorf("%s should ignore the index", c.name)
			}
			// The name argument is currently ignored by every dialect; pin that contract.
			if got := ph(1, "some_name"); got != c.at1 {
				t.Errorf("placeholder(1, name) = %q, want %q (name must be ignored)", got, c.at1)
			}
		})
	}
}

func TestFormatLiteral(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{"", "''"},
		{"plain", "'plain'"},
		{"O'Brien", "'O''Brien'"},
		{true, "true"},
		{false, "false"},
		{42, "42"},
		{int8(5), "5"},
		{int64(-7), "-7"},
		{uint(9), "9"},
		{float64(1.5), "1.5"},
		{float32(2.5), "2.5"},
	}
	for _, c := range cases {
		got, err := dialect.FormatLiteral(c.v)
		if err != nil {
			t.Errorf("FormatLiteral(%#v): %v", c.v, err)
			continue
		}
		if got != c.want {
			t.Errorf("FormatLiteral(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestFormatLiteralUnsupported(t *testing.T) {
	cases := []struct {
		v     any
		inMsg string
	}{
		{[]byte{1, 2}, "[]uint8"},
		{struct{ X int }{}, "struct"},
	}
	for _, c := range cases {
		_, err := dialect.FormatLiteral(c.v)
		if err == nil {
			t.Errorf("FormatLiteral(%T): expected error", c.v)
			continue
		}
		if !strings.Contains(err.Error(), c.inMsg) {
			t.Errorf("error %q should mention %q", err, c.inMsg)
		}
	}
}

// A dialect without its own formatter falls back to FormatLiteral.
func TestDialectLiteralFallback(t *testing.T) {
	got, err := dialect.MySQL.Literal()("x")
	if err != nil {
		t.Fatal(err)
	}
	if got != "'x'" {
		t.Errorf("got %q", got)
	}
}

// PostgreSQL supplies its own literal formatter that renders slices/arrays as PostgreSQL array
// literals, and delegates scalars to FormatLiteral. This exercises the non-fallback branch of
// Dialect.Literal().
func TestPostgresArrayLiteral(t *testing.T) {
	lit := dialect.PostgreSQL.Literal()
	cases := []struct {
		v    any
		want string
	}{
		{[]int{10, 20, 30}, "'{10,20,30}'"},
		{[]any{}, "'{}'"},
		{[]any{"a", "b,c"}, `'{"a","b,c"}'`},
		{[]any{"o'brien"}, `'{"o''brien"}'`}, // SQL '' escaping around the array text
		{[]any{nil, 1, true}, "'{NULL,1,true}'"},
		{[]any{[]any{1, 2}, []any{3, 4}}, "'{{1,2},{3,4}}'"}, // nested / multi-dimensional
		{"scalar", "'scalar'"},                               // scalars delegate to FormatLiteral
		{42, "42"},
		{[]byte{1, 2}, ""}, // []byte is a scalar bytea, not an array -> FormatLiteral errors
	}
	for _, c := range cases {
		got, err := lit(c.v)
		if c.v != nil {
			if _, isBytes := c.v.([]byte); isBytes {
				if err == nil {
					t.Errorf("[]byte should error (scalar bytea), got %q", got)
				}
				continue
			}
		}
		if err != nil {
			t.Errorf("literal(%#v): %v", c.v, err)
			continue
		}
		if got != c.want {
			t.Errorf("literal(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// An array element the formatter cannot render (a struct) propagates its error out of the
// array formatter, both for a flat array and through the recursive nested-array path.
func TestPostgresArrayLiteralElementError(t *testing.T) {
	lit := dialect.PostgreSQL.Literal()

	if _, err := lit([]any{struct{}{}}); err == nil {
		t.Error("flat array with an unformattable struct element should error")
	}
	if _, err := lit([][]any{{struct{}{}}}); err == nil {
		t.Error("nested array with an unformattable struct element should error")
	}
}
