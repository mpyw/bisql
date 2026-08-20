package bindsyntax_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql/bindsyntax"
)

func TestRecognize(t *testing.T) {
	cases := []struct {
		in   string
		name string
		kind bindsyntax.Kind
		n    int
	}{
		{"@status", "status", bindsyntax.Arg, 7},
		{"@status and x = 1", "status", bindsyntax.Arg, 7},
		{"@_leading", "_leading", bindsyntax.Arg, 9},
		{"@a1", "a1", bindsyntax.Arg, 3},
		{"sqlc.arg('status')", "status", bindsyntax.Arg, 18},
		{"sqlc.narg('note')", "note", bindsyntax.NArg, 17},
		{"sqlc.slice('ids')", "ids", bindsyntax.Slice, 17},
		{"sqlc.arg('c.name')", "c.name", bindsyntax.Arg, 18},
		{"sqlc.arg( 'x' )", "x", bindsyntax.Arg, 15},
		// A cast follows the marker and is not part of it.
		{"sqlc.arg('x')::text", "x", bindsyntax.Arg, 13},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			m, ok := bindsyntax.Recognize(c.in)
			if !ok {
				t.Fatalf("Recognize(%q) = not recognized", c.in)
			}
			if m.Name != c.name || m.Kind != c.kind || m.Len != c.n {
				t.Errorf("Recognize(%q) = %+v, want {Name:%q Kind:%v Len:%d}", c.in, m, c.name, c.kind, c.n)
			}
			if got := c.in[:m.Len]; len(got) != c.n {
				t.Errorf("Len %d does not span a prefix of %q", m.Len, c.in)
			}
		})
	}
}

func TestRecognizeRejects(t *testing.T) {
	// @a.b is rejected because sqlc rejects it too: a dotted name has to be written
	// as sqlc.arg('a.b'). Recognizing @a here would silently bind something else.
	for _, in := range []string{
		"", "@", "@1abc", "@ ", "status", "'@status'",
		"sqlc.arg()", "sqlc.arg('')", "sqlc.arg(x)", "sqlc.arg('x'", "sqlc.args('x')",
		"sqlc.arg(\"x\")", "sqlc.slice('x'", "@@x",
	} {
		t.Run(in, func(t *testing.T) {
			if m, ok := bindsyntax.Recognize(in); ok {
				t.Errorf("Recognize(%q) = %+v, want not recognized", in, m)
			}
		})
	}
}

// @a.b stops at the dot, which is the reading sqlc makes too — and why Recognize alone is
// not enough to tell a bind from a mistake.
func TestRecognizeStopsAtDot(t *testing.T) {
	m, ok := bindsyntax.Recognize("@a.b")
	if !ok || m.Name != "a" || m.Len != 2 {
		t.Errorf("Recognize(\"@a.b\") = %+v, %v; want name a spanning 2 bytes", m, ok)
	}
}

// Malformed is what makes the two silent mistakes loud. Without it @c.name renders as
// "$1.name" and sqlc.arg(x) is emitted verbatim as a call to a function that does not
// exist, both without any complaint.
func TestMalformed(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"@c.name", "dotted bind name"},
		{"@c.name = 1", "dotted bind name"},
		{"sqlc.arg(x)", "single-quoted name"},
		{`sqlc.arg("x")`, "single-quoted name"},
		{"sqlc.arg()", "single-quoted name"},
		{"sqlc.arg('')", "single-quoted name"},
		{"sqlc.arg('x'", "single-quoted name"},
		{"sqlc.narg(x)", "single-quoted name"},
		{"sqlc.slice(x)", "single-quoted name"},
	} {
		t.Run(c.in, func(t *testing.T) {
			reason, bad := bindsyntax.Malformed(c.in)
			if !bad {
				t.Fatalf("Malformed(%q) = not malformed", c.in)
			}
			if !strings.Contains(reason, c.want) {
				t.Errorf("reason = %q, want it to contain %q", reason, c.want)
			}
		})
	}
}

// A well-formed marker is not malformed, and neither is anything that was never a marker:
// @ and sqlc. both occur in ordinary SQL.
func TestMalformedLeavesEverythingElseAlone(t *testing.T) {
	for _, in := range []string{
		"@status", "@status = 1", "sqlc.arg('x')", "sqlc.narg('c.note')", "sqlc.slice('ids')",
		"", "@", "@>", "@@version", "tags @> '{a}'", "status", "sqlc.args('x')", "sqlc_arg('x')",
	} {
		t.Run(in, func(t *testing.T) {
			if reason, bad := bindsyntax.Malformed(in); bad {
				t.Errorf("Malformed(%q) = %q, want not malformed", in, reason)
			}
		})
	}
}

func TestStrings(t *testing.T) {
	if got := bindsyntax.TwoWay.String(); got != "two-way" {
		t.Errorf("TwoWay = %q", got)
	}
	if got := bindsyntax.SqlcNamed.String(); got != "sqlc-named" {
		t.Errorf("SqlcNamed = %q", got)
	}
	if got := bindsyntax.NArg.String(); got != "narg" {
		t.Errorf("NArg = %q", got)
	}
}
