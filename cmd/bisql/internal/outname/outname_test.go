package outname_test

import (
	"testing"

	"github.com/mpyw/bisql/cmd/bisql/internal/outname"
)

func TestRender(t *testing.T) {
	cases := []struct {
		name, format, rel, want string
	}{
		{"default mirrors", "", "employees/search.sql", "employees/search.sql"},
		{"suffix keeps tree", "{{.Dir}}/{{.Name}}.gen.sql", "employees/search.sql", "employees/search.gen.sql"},
		{"flatten to base", "{{.Base}}", "employees/search.sql", "search.sql"},
		{"root-level file", "{{.Dir}}/{{.Name}}.gen.sql", "top.sql", "top.gen.sql"},
		{"change extension", "{{.Name}}.txt", "a/b.sql", "b.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := outname.Parse(c.format)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got, err := f.Render(c.rel)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != c.want {
				t.Errorf("Render(%q) = %q, want %q", c.rel, got, c.want)
			}
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	if _, err := outname.Parse("{{.Nope"); err == nil {
		t.Error("an unterminated template should fail to parse")
	}
}

func TestRender_Rejects(t *testing.T) {
	cases := []struct{ name, format string }{
		{"escapes with ..", "../{{.Base}}"},
		{"empty result", "{{if false}}x{{end}}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := outname.Parse(c.format)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if _, err := f.Render("a/b.sql"); err == nil {
				t.Error("expected Render to reject the output path")
			}
		})
	}
}
