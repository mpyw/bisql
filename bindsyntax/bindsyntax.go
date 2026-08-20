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

import (
	"fmt"
	"strings"
)

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

// Rules are the bind spellings a template may use, resolved from the syntax and the
// dialect. The resolution exists because one spelling is not available everywhere: sqlc
// supports @name as a shortcut for sqlc.arg(name) for every engine except MySQL, where
// @name is a user variable. bisql has to read a template exactly the way sqlc does — a
// spelling one of them binds and the other does not is the one failure this design is
// built to avoid — so the shortcut has to be dialect-dependent here too.
type Rules struct {
	Syntax Syntax
	AtForm bool // whether a bare @name is a bind
}

// RulesFor resolves the rules for a syntax and a dialect name (as dialect.Dialect.Name
// reports it).
func RulesFor(s Syntax, dialectName string) Rules {
	return Rules{Syntax: s, AtForm: s == SqlcNamed && dialectName != "mysql"}
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

// callForms are the wrapper calls, longest-distinguishing prefix first so that narg is not
// read as a suffix of anything.
var callForms = []struct {
	prefix string
	kind   Kind
}{
	{"sqlc.arg(", Arg},
	{"sqlc.narg(", NArg},
	{"sqlc.slice(", Slice},
}

// Recognize reports the bind marker at the start of s, if there is one. It recognizes
// exactly what sqlc recognizes, including that a bare @name ends at the first character
// that cannot continue an identifier — so @a.b yields the marker @a, as it does for sqlc.
// Malformed is what tells a caller that such a prefix was a mistake rather than a bind.
//
// Recognize looks only at s's prefix and never scans ahead, so a caller that already tracks
// quotes and comments — as a lexer does — can consult it at each position without giving up
// that tracking.
func (r Rules) Recognize(s string) (Marker, bool) {
	if r.Syntax != SqlcNamed {
		return Marker{}, false
	}
	if r.AtForm {
		if name, n, ok := atName(s); ok {
			return Marker{Name: name, Kind: Arg, Len: n}, true
		}
	}
	for _, form := range callForms {
		if !strings.HasPrefix(s, form.prefix) {
			continue
		}
		name, n, ok := callArgument(s[len(form.prefix):])
		if !ok {
			return Marker{}, false
		}
		return Marker{Name: name, Kind: form.kind, Len: len(form.prefix) + n}, true
	}
	return Marker{}, false
}

// Malformed reports a reason when s begins with something that can only have been meant as
// a bind marker but cannot be one, so that a caller can fail on the mistake instead of
// passing it through.
//
// Both cases it catches would otherwise become SQL that looks plausible and is not. A
// dotted @a.b binds only "a" and leaves ".b" behind, which renders as "$1.b"; sqlc makes
// the same reading and then rejects the result, but a renderer that never parses SQL has
// nothing to reject it with. A call form whose argument is not a single-quoted name matches
// nothing and is emitted verbatim, becoming a call to a function that does not exist.
//
// It should be consulted before Recognize, since Recognize accepts the leading @a of a
// dotted name.
func (r Rules) Malformed(s string) (string, bool) {
	if r.Syntax != SqlcNamed {
		return "", false
	}
	if name, n, ok := atName(s); ok && r.AtForm {
		if n < len(s) && s[n] == '.' {
			return fmt.Sprintf("@%s is followed by a period: a dotted bind name has to be "+
				"written as sqlc.arg('%s.…'), because @%s.… reads as the parameter @%s and "+
				"then trailing text", name, name, name, name), true
		}
		return "", false
	}
	for _, form := range callForms {
		if !strings.HasPrefix(s, form.prefix) {
			continue
		}
		rest := s[len(form.prefix):]
		if _, _, ok := callArgument(rest); ok {
			return "", false
		}
		// An unquoted dotted name is the likeliest mistake, and it has a better fix than
		// the general one: quote it.
		if name, n, ok := leadingIdent(rest); ok && n < len(rest) && rest[n] == '.' {
			return fmt.Sprintf("%s%s.…) has a dotted name, which has to be quoted: %s'%s.…')",
				form.prefix, name, form.prefix, name), true
		}
		return fmt.Sprintf("%s…) takes a name, either bare as %sname) or quoted as %s'a.name')",
			form.prefix, form.prefix, form.prefix), true
	}
	return "", false
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

// callArgument reads the argument of a wrapper call, `name)` or `'name')`, allowing space
// around it. sqlc accepts both spellings but only the quoted one may carry dots — an
// unquoted a.b parses as a column reference and sqlc rejects it — so the same holds here.
func callArgument(s string) (string, int, bool) {
	if name, n, ok := bareName(s); ok {
		return name, n, true
	}
	return quotedName(s)
}

// bareName reads `name)`: an unquoted identifier, with no dots.
func bareName(s string) (string, int, bool) {
	name, i, ok := leadingIdent(s)
	if !ok {
		return "", 0, false
	}
	i = skipSpace(s, i)
	if i >= len(s) || s[i] != ')' {
		return "", 0, false
	}
	return name, i + 1, true
}

// leadingIdent reads the identifier at the start of s, after any space, and reports how far
// it read. It does not care what follows, which is what lets a caller tell `name)` from
// `name.other)`.
func leadingIdent(s string) (string, int, bool) {
	i := skipSpace(s, 0)
	start := i
	for i < len(s) && isIdent(s[i], i == start) {
		i++
	}
	if i == start {
		return "", 0, false
	}
	return s[start:i], i, true
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
