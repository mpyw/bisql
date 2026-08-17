package parser

import "testing"

// White-box tests for the unexported for-separator helpers. These assert the *interpretation*
// of the `: 'sep'` clause directly, which the lossless round-trip cannot cover (ForDirective
// re-emits the raw token, so a parse-and-Text() check never reads back the split values).

func TestSplitForSeparator(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		wantIter string
		wantSep  string
		wantErr  bool
	}{
		{"no separator", "xs", "xs", "", false},
		{"single-quoted", "xs : ', '", "xs", ", ", false},
		{"double-quoted", `xs : ", "`, "xs", ", ", false},
		{"doubled-quote escape", "xs : ''''", "xs", "'", false},
		{"doubled-quote escape twice", "xs : ''''''", "xs", "''", false},
		{"empty literal", "xs : ''", "xs", "", false},
		{"separator is a colon", "xs : ':'", "xs", ":", false},
		{"colon inside quote", "'a:b'", "'a:b'", "", false},
		{"colon inside bracket", "xs[1:2]", "xs[1:2]", "", false},
		{"colon inside paren", "f(a:b)", "f(a:b)", "", false},
		{"colon inside brace", "{k:v}", "{k:v}", "", false},
		{"unterminated quote", "xs : 'abc", "", "", true},
		{"mismatched quotes", `xs : 'abc"`, "", "", true},
		{"trailing bytes after literal", "xs : 'a'b", "", "", true},
		{"unquoted separator", "xs : bad", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			iter, sep, err := splitForSeparator(c.expr)
			if c.wantErr {
				if err == nil {
					t.Fatalf("splitForSeparator(%q): expected error, got iter=%q sep=%q", c.expr, iter, sep)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitForSeparator(%q): unexpected error: %v", c.expr, err)
			}
			if iter != c.wantIter {
				t.Errorf("splitForSeparator(%q) iter = %q, want %q", c.expr, iter, c.wantIter)
			}
			if sep != c.wantSep {
				t.Errorf("splitForSeparator(%q) sep = %q, want %q", c.expr, sep, c.wantSep)
			}
		})
	}
}

func TestUnquote(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"single-quoted", "'ab'", "ab", true},
		{"double-quoted", `"ab"`, "ab", true},
		{"doubled-quote escape", "''''", "'", true},
		{"doubled-quote escape twice", "''''''", "''", true},
		{"doubled-quote escape inside", "'a''b'", "a'b", true},
		{"empty single", "''", "", true},
		{"empty double", `""`, "", true},
		{"unterminated quote", "'ab", "", false},
		{"mismatched quotes", `'ab"`, "", false},
		{"trailing bytes after literal", "'a'b", "", false},
		{"unescaped quote ends early", "'a'b'", "", false},
		{"too short", "'", "", false},
		{"empty string", "", "", false},
		{"not a quote", "ab", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := unquote(c.in)
			if ok != c.wantOK {
				t.Fatalf("unquote(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Errorf("unquote(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
