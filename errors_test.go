package bisql_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql"
)

// Parse errors: malformed templates are rejected at Parse time.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
	}{
		{"unbalanced open paren", "select * from (select 1"},
		{"stray close paren", "select 1)"},
		{"if without end", "select 1 where /*%if a*/x = 1"},
		{"end without block", "select 1 /*%end*/"},
		{"elseif without if", "select 1 /*%elseif a*/x/*%end*/"},
		{"empty bind expression", "select /* */x"},
		{"for without in", "select /*%for x*/y/*%end*/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := bisql.Parse(c.tmpl); err == nil {
				t.Fatalf("Parse(%q): expected error", c.tmpl)
			}
		})
	}
}

// Build errors: problems that only surface once the tree is evaluated with a scope.
func TestBuildErrors(t *testing.T) {
	cases := []struct {
		name   string
		tmpl   string
		params map[string]any
		want   string // substring expected in the error
	}{
		{
			name:   "if condition not boolean",
			tmpl:   "select 1 where /*%if a*/x = 1/*%end*/",
			params: map[string]any{"a": 5},
			want:   "not a boolean",
		},
		{
			name:   "for over non-iterable",
			tmpl:   "select /*%for i in a*/x/*%end*/",
			params: map[string]any{"a": 5},
			want:   "not iterable",
		},
		{
			name:   "bind expression evaluation fails",
			tmpl:   "select * where a = /*a.b.c*/0",
			params: map[string]any{"a": 1},
			want:   "evaluating",
		},
		{
			name:   "partial without a loader",
			tmpl:   "select 1 where /*> frag */",
			params: nil,
			want:   "requires a Loader",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(c.tmpl)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, err = tmpl.Build(c.params)
			if err == nil {
				t.Fatalf("Build: expected error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

// Unknown partial names are reported at Build time.
func TestUnknownPartial(t *testing.T) {
	ld := bisql.NewLoader()
	ld.Register("known", "x = 1")
	tmpl, err := ld.Parse("select 1 where /*> unknown */")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tmpl.Build(nil)
	if err == nil || !strings.Contains(err.Error(), "unknown partial") {
		t.Fatalf("expected unknown-partial error, got %v", err)
	}
}

// A self-referential embedded value is bounded by the depth guard rather than looping.
func TestEmbeddedDepthGuard(t *testing.T) {
	tmpl, err := bisql.Parse("select /*# self */")
	if err != nil {
		t.Fatal(err)
	}
	// The value reproduces an embedded reference to itself, so expansion never terminates
	// on its own; the depth guard must stop it.
	_, err = tmpl.Build(map[string]any{"self": "/*# self */"})
	if err == nil || !strings.Contains(err.Error(), "depth exceeded") {
		t.Fatalf("expected depth-exceeded error, got %v", err)
	}
}

// Invalid parameter types are rejected.
func TestInvalidParams(t *testing.T) {
	tmpl, err := bisql.Parse("select 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Build(42); err == nil {
		t.Fatal("expected error for int params")
	}
	if _, err := tmpl.Build([]string{"a"}); err == nil {
		t.Fatal("expected error for slice params")
	}
}
