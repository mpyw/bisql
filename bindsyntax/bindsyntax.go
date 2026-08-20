// Package bindsyntax selects how a bind is written in a template.
//
// bisql's own syntax puts the bind in a comment and leaves a sample literal in
// the SQL — `/*status*/'active'` — which is what makes a template runnable as-is
// in a client: the comment is ignored and the literal takes its place. That
// property is why the syntax looks the way it does.
//
// The cost of it shows up when a template is also meant to be read by a static
// analyzer such as sqlc. Being runnable requires a *literal* at the bind site;
// being recognized as a parameter requires a *marker*. No single text is both, so
// a template written the two-way way is one whose binds an analyzer sees as
// constants — it can check the SQL and the result columns, but it can tell you
// nothing about the arguments.
//
// SqlcNamed gives up the runnable-as-is property for values, and only for
// values, in exchange for that. Structure directives are unaffected either way:
// `/*%if*/` and `/*%for*/` are comments under any bind syntax, so an analyzer
// skips them without needing to understand them. What it buys is that every bind
// becomes a parameter the analyzer resolves against the catalog, with a name
// attached.
//
//	two-way      where status = /*status*/'active'
//	sqlc-named   where status = @status
//	             where name like sqlc.arg('c.name')   -- a dotted name, for a loop element
//
// Recognize is the seam a lexer calls: it decides whether a bind marker starts
// here, and says nothing about the surrounding SQL.
package bindsyntax

import "strings"

// Syntax is a choice of bind spelling.
type Syntax uint8

const (
	// TwoWay is bisql's own syntax, `/*expr*/literal`, and the default. A template
	// written this way runs unmodified in a SQL client.
	TwoWay Syntax = iota

	// SqlcNamed is sqlc's syntax: `@name`, `sqlc.arg('name')`, `sqlc.narg('name')`,
	// `sqlc.slice('name')`. A template written this way is not runnable as-is, but
	// its binds are parameters a static analyzer can resolve and type.
	SqlcNamed
)

func (s Syntax) String() string {
	switch s {
	case TwoWay:
		return "two-way"
	case SqlcNamed:
		return "sqlc-named"
	}
	return "unknown"
}

// Kind distinguishes the SqlcNamed forms, which differ in what they promise about
// the value rather than in how it is spelled.
type Kind uint8

const (
	// Arg is a plain parameter: `@name` or `sqlc.arg('name')`.
	Arg Kind = iota
	// NArg is a parameter that may be null: `sqlc.narg('name')`.
	NArg
	// Slice is a parameter that expands to a placeholder list: `sqlc.slice('name')`.
	Slice
)

func (k Kind) String() string {
	switch k {
	case Arg:
		return "arg"
	case NArg:
		return "narg"
	case Slice:
		return "slice"
	}
	return "unknown"
}

// Marker is a recognized bind: the name it binds, which form it took, and how
// many bytes of the input it spans.
type Marker struct {
	Name string
	Kind Kind
	Len  int
}

// Recognize reports the bind marker at the start of s, if there is one.
//
// It looks only at s's prefix and never scans ahead, so a caller that already
// tracks quotes and comments — as a lexer does — can consult it at each position
// without giving up that tracking.
func Recognize(s string) (Marker, bool) {
	if name, n, ok := atName(s); ok {
		return Marker{Name: name, Kind: Arg, Len: n}, true
	}
	for _, form := range []struct {
		prefix string
		kind   Kind
	}{
		{"sqlc.arg(", Arg},
		{"sqlc.narg(", NArg},
		{"sqlc.slice(", Slice},
	} {
		if !strings.HasPrefix(s, form.prefix) {
			continue
		}
		name, n, ok := quotedName(s[len(form.prefix):])
		if !ok {
			return Marker{}, false
		}
		return Marker{Name: name, Kind: form.kind, Len: len(form.prefix) + n}, true
	}
	return Marker{}, false
}

// atName reads an `@name` marker. The name is a bare identifier: a dotted name
// has to be written as sqlc.arg('a.b'), because `@a.b` is not valid input to sqlc
// either.
func atName(s string) (string, int, bool) {
	if !strings.HasPrefix(s, "@") {
		return "", 0, false
	}
	i := 1
	for i < len(s) && isIdent(s[i], i == 1) {
		i++
	}
	if i == 1 {
		return "", 0, false
	}
	return s[1:i], i, true
}

// quotedName reads `'name')`, allowing space around the literal. The name is
// taken verbatim up to the closing quote, so it may contain dots.
func quotedName(s string) (string, int, bool) {
	i := skipSpace(s, 0)
	if i >= len(s) || s[i] != '\'' {
		return "", 0, false
	}
	i++
	start := i
	for i < len(s) && s[i] != '\'' {
		i++
	}
	if i >= len(s) || i == start {
		return "", 0, false
	}
	name := s[start:i]
	i++ // closing quote
	i = skipSpace(s, i)
	if i >= len(s) || s[i] != ')' {
		return "", 0, false
	}
	return name, i + 1, true
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// isIdent reports whether c may appear in an identifier, at the first position or
// after it.
func isIdent(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}
