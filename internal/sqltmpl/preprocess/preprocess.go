// Package preprocess implements the @include preprocessor. A /*%! @include name */ parser
// comment is replaced, recursively, by the named fragment's raw text — producing a fully
// expanded (still 2-way) template that is then lexed/parsed/rendered normally. Because the
// directive rides on the /*%! ... */ parser-comment channel, a raw template pasted into a
// SQL client treats it as a comment (the base SQL runs without the fragment).
package preprocess

import (
	"fmt"
	"strings"
)

// DefaultMaxDepth bounds recursive expansion (a backstop beyond cycle detection).
const DefaultMaxDepth = 50

// Resolver returns the raw text of a named fragment, or an error if unknown.
type Resolver func(name string) (string, error)

// Expand replaces every /*%! @include name */ in src with the resolved fragment text,
// recursively. It is string- and comment-aware (an @include inside a '...' literal or a
// -- line comment is left untouched). Cyclic references and unknown fragments error.
func Expand(src string, resolve Resolver) (string, error) {
	return expand(src, resolve, map[string]bool{}, 0)
}

func expand(src string, resolve Resolver, active map[string]bool, depth int) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			// '...' string / "..." / `...` quoted identifier: opaque, skip its contents so
			// an inner @include-looking comment or quote is not touched.
			j := scanQuoted(src, i)
			b.WriteString(src[i:j])
			i = j
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			j := i + 2
			for j < len(src) && src[j] != '\n' && src[j] != '\r' {
				j++
			}
			b.WriteString(src[i:j])
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := indexStarSlash(src, i+2)
			if end < 0 {
				// Unterminated block comment: leave it; the lexer reports the error later.
				b.WriteString(src[i:])
				return b.String(), nil
			}
			name, ok, err := includeName(src[i+2 : end-2])
			if err != nil {
				return "", err
			}
			if !ok {
				b.WriteString(src[i:end]) // ordinary comment / directive: copy through
				i = end
				continue
			}
			if active[name] {
				return "", fmt.Errorf("bisql/preprocess: cyclic @include reference %q", name)
			}
			if depth >= DefaultMaxDepth {
				return "", fmt.Errorf("bisql/preprocess: @include depth exceeded %d", DefaultMaxDepth)
			}
			frag, err := resolve(name)
			if err != nil {
				return "", err
			}
			active[name] = true
			exp, err := expand(frag, resolve, active, depth+1)
			delete(active, name)
			if err != nil {
				return "", err
			}
			b.WriteString(exp)
			i = end
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), nil
}

// scanQuoted returns the index just past a quoted span opened by src[i] (', ", or `). The
// quote is escaped by doubling; an unterminated span consumes to end.
func scanQuoted(src string, i int) int {
	quote := src[i]
	j := i + 1
	for j < len(src) {
		if src[j] == quote {
			if j+1 < len(src) && src[j+1] == quote {
				j += 2
				continue
			}
			return j + 1
		}
		j++
	}
	return len(src)
}

// indexStarSlash returns the index just past the next "*/" at or after from, or -1.
func indexStarSlash(src string, from int) int {
	k := strings.Index(src[from:], "*/")
	if k < 0 {
		return -1
	}
	return from + k + 2
}

// includeName inspects a block comment's inner text (between /* and */). It returns the
// fragment name when the block is a /*%! @include name */ directive.
func includeName(content string) (name string, ok bool, err error) {
	t := strings.TrimSpace(content)
	if !strings.HasPrefix(t, "%!") {
		return "", false, nil // not a parser comment
	}
	rest := strings.TrimSpace(t[len("%!"):])
	const kw = "@include"
	if !strings.HasPrefix(rest, kw) {
		return "", false, nil // a plain /*%! ... */ parser comment
	}
	after := rest[len(kw):]
	if after != "" && !isSpace(after[0]) {
		return "", false, nil // e.g. @includes... : not the include directive
	}
	name = strings.TrimSpace(after)
	if name == "" {
		return "", false, fmt.Errorf("bisql/preprocess: @include requires a fragment name")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "", false, fmt.Errorf("bisql/preprocess: @include name %q must be a single token", name)
	}
	return name, true, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
