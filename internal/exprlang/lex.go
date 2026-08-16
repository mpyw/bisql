package exprlang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type tkind int

const (
	tEOF tkind = iota
	tNumber
	tString
	tIdent
	tNull
	tTrue
	tFalse
	tDot
	tSafeDot // ?.
	tLParen
	tRParen
	tComma
	tNot
	tAnd
	tOr
	tEq
	tNe
	tGt
	tLt
	tGe
	tLe
)

type tok struct {
	kind tkind
	text string
	num  any // int64 or float64 for tNumber; string value for tString
}

func lex(s string) ([]tok, error) {
	var out []tok
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
			continue
		case c == '(':
			out = append(out, tok{kind: tLParen, text: "("})
			i++
		case c == ')':
			out = append(out, tok{kind: tRParen, text: ")"})
			i++
		case c == ',':
			out = append(out, tok{kind: tComma, text: ","})
			i++
		case c == '.':
			out = append(out, tok{kind: tDot, text: "."})
			i++
		case c == '?' && i+1 < len(s) && s[i+1] == '.':
			out = append(out, tok{kind: tSafeDot, text: "?."})
			i += 2
		case c == '&' && i+1 < len(s) && s[i+1] == '&':
			out = append(out, tok{kind: tAnd, text: "&&"})
			i += 2
		case c == '|' && i+1 < len(s) && s[i+1] == '|':
			out = append(out, tok{kind: tOr, text: "||"})
			i += 2
		case c == '=' && i+1 < len(s) && s[i+1] == '=':
			out = append(out, tok{kind: tEq, text: "=="})
			i += 2
		case c == '!' && i+1 < len(s) && s[i+1] == '=':
			out = append(out, tok{kind: tNe, text: "!="})
			i += 2
		case c == '>' && i+1 < len(s) && s[i+1] == '=':
			out = append(out, tok{kind: tGe, text: ">="})
			i += 2
		case c == '<' && i+1 < len(s) && s[i+1] == '=':
			out = append(out, tok{kind: tLe, text: "<="})
			i += 2
		case c == '!':
			out = append(out, tok{kind: tNot, text: "!"})
			i++
		case c == '>':
			out = append(out, tok{kind: tGt, text: ">"})
			i++
		case c == '<':
			out = append(out, tok{kind: tLt, text: "<"})
			i++
		case c == '"' || c == '\'':
			str, n, err := lexString(s[i:], c)
			if err != nil {
				return nil, err
			}
			out = append(out, tok{kind: tString, text: s[i : i+n], num: str})
			i += n
		case c >= '0' && c <= '9':
			t, n, err := lexNumber(s[i:])
			if err != nil {
				return nil, err
			}
			out = append(out, t)
			i += n
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if isIdentStart(r) {
				j := i + size
				for j < len(s) {
					r2, sz := utf8.DecodeRuneInString(s[j:])
					if !isIdentPart(r2) {
						break
					}
					j += sz
				}
				word := s[i:j]
				out = append(out, identToken(word))
				i = j
			} else {
				return nil, fmt.Errorf("bisql/exprlang: illegal character %q in expression", string(r))
			}
		}
	}
	out = append(out, tok{kind: tEOF})
	return out, nil
}

func lexString(s string, quote byte) (string, int, error) {
	// s[0] == quote. Supports doubled-quote escaping ('' or "").
	var b strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				b.WriteByte(quote)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", 0, fmt.Errorf("bisql/exprlang: unterminated string literal")
}

func lexNumber(s string) (tok, int, error) {
	i := 0
	dot := false
	for i < len(s) {
		c := s[i]
		if c >= '0' && c <= '9' {
			i++
			continue
		}
		if c == '.' && !dot {
			dot = true
			i++
			continue
		}
		break
	}
	text := s[:i]
	if dot {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return tok{}, 0, fmt.Errorf("bisql/exprlang: illegal number %q", text)
		}
		return tok{kind: tNumber, text: text, num: f}, i, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return tok{}, 0, fmt.Errorf("bisql/exprlang: illegal number %q", text)
	}
	return tok{kind: tNumber, text: text, num: n}, i, nil
}

func identToken(word string) tok {
	switch word {
	case "null":
		return tok{kind: tNull, text: word}
	case "true":
		return tok{kind: tTrue, text: word}
	case "false":
		return tok{kind: tFalse, text: word}
	default:
		return tok{kind: tIdent, text: word}
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}
