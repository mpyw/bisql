package dialect_test

import (
	"testing"

	"github.com/mpyw/bisql/dialect"
)

func TestNamesAndPlaceholders(t *testing.T) {
	cases := []struct {
		d    dialect.Dialect
		name string
		ph   string // placeholder for index 3
	}{
		{dialect.MySQL, "mysql", "?"},
		{dialect.PostgreSQL, "postgresql", "$3"},
		{dialect.Oracle, "oracle", ":3"},
		{dialect.SQLServer, "sqlserver", "@p3"},
	}
	for _, c := range cases {
		if got := c.d.Name(); got != c.name {
			t.Errorf("Name() = %q, want %q", got, c.name)
		}
		if got := c.d.Placeholder()(3, ""); got != c.ph {
			t.Errorf("%s placeholder = %q, want %q", c.name, got, c.ph)
		}
	}
}

func TestFormatLiteral(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{"plain", "'plain'"},
		{"O'Brien", "'O''Brien'"},
		{true, "true"},
		{false, "false"},
		{42, "42"},
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
	if _, err := dialect.FormatLiteral([]byte{1, 2}); err == nil {
		t.Error("expected error for []byte")
	}
	if _, err := dialect.FormatLiteral(struct{ X int }{}); err == nil {
		t.Error("expected error for struct")
	}
}

// The default literal formatter is used when a dialect defines none.
func TestDialectLiteralFallback(t *testing.T) {
	got, err := dialect.MySQL.Literal()("x")
	if err != nil {
		t.Fatal(err)
	}
	if got != "'x'" {
		t.Errorf("got %q", got)
	}
}
