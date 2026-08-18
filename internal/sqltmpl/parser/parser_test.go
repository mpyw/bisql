package parser_test

import (
	"strings"
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
	"github.com/mpyw/bisql/internal/sqltmpl/parser"
)

// The tree is lossless: Text() reproduces the input, except parser comments (/*%!) and a
// trailing delimiter (;) are dropped.
//
// Every case is also a *valid 2-way template*: with the directives read as SQL comments (as
// a DB client would), the raw text is runnable SQL. That means the branch/loop bodies are
// composing (each carries a leading `and`/`or`) and anchored (`1 = 1` / `1 = 0` / a trailing
// key), so nothing dangles — the same discipline bisql asks of authors.
func TestParse_Lossless(t *testing.T) {
	cases := []string{
		"",
		"select 1",
		"select * from t where a = /*a*/'x' and b > /*b*/0",
		"select id from t where id in /*ids*/(1, 2, 3)",
		"select * from (select id from t) x",
		// if/elseif/else with composing (`and ...`) bodies + a 1=1 anchor: raw text is
		// `where 1 = 1 and x = 1 and y = 2 and z = 3`, which is valid.
		"select * from t where 1 = 1 /*%if a*/ and x = 1/*%elseif b*/ and y = 2/*%else*/ and z = 3/*%end*/",
		// for-loop with a 1=0 OR-anchor and a self-contained `or ...` body: raw text is
		// `where 1 = 0 or name like '%x%'`, which is valid.
		"select * from t where 1 = 0 /*%for kw in kws*/or name like /*kw*/'%x%'/*%end*/",
		// for-loop building a comma list via a WHERE 1=0 UNION ALL seed and a self-contained
		// `union all select ...` body: raw text is `select 0 x where 1 = 0 union all select 0`.
		"select 0 x where 1 = 0 /*%for v in vals*/union all select /*v*/0/*%end*/",
		"select /** keep */ /*# also a comment now */ 1 -- trailing\nfrom t",
		"select * from t where a = /*a*/1::bigint and b = ANY(/*b*/'{}'::int[])",
		"select `a`, \"b\", 'c/*x*/' from t",
	}
	for _, src := range cases {
		n, err := parser.Parse(src)
		if err != nil {
			t.Errorf("parser.Parse(%q): %v", src, err)
			continue
		}
		if got := n.Text(); got != src {
			t.Errorf("lossless mismatch\n got: %q\nwant: %q", got, src)
		}
	}
}

func TestParse_ParserCommentAndDelimiterDropped(t *testing.T) {
	n, err := parser.Parse("select 1 /*%! note */ from t; select 2")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Text(); got != "select 1  from t" {
		t.Errorf("got %q", got)
	}
}

func TestParse_Structure(t *testing.T) {
	// Note: a bind directive folds any following nodes into its Trailing until a reduce
	// boundary, so put the if-block first to keep both at the top level.
	n, err := parser.Parse("/*%if p*/y/*%end*/ /*a*/'v'")
	if err != nil {
		t.Fatal(err)
	}
	st, ok := n.(ast.Statement)
	if !ok {
		t.Fatalf("root is %T, want Statement", n)
	}
	var sawBind, sawIf bool
	for _, c := range st.Nodes {
		switch b := c.(type) {
		case ast.BindValue:
			sawBind = true
			if b.Expression != "a" {
				t.Errorf("bind expr = %q", b.Expression)
			}
			if _, isWord := b.Test.(ast.Word); !isWord {
				t.Errorf("bind test = %T, want Word", b.Test)
			}
		case ast.IfBlock:
			sawIf = true
		}
	}
	if !sawBind || !sawIf {
		t.Errorf("missing nodes: bind=%v if=%v", sawBind, sawIf)
	}
}

// A bind's paren test literal parses as a Paren (this drives IN-list expansion at render).
func TestParse_ParenTest(t *testing.T) {
	n, err := parser.Parse("id in /*ids*/(1, 2)")
	if err != nil {
		t.Fatal(err)
	}
	st := n.(ast.Statement)
	for _, c := range st.Nodes {
		if b, ok := c.(ast.BindValue); ok {
			if _, isParen := b.Test.(ast.Paren); !isParen {
				t.Errorf("paren-test bind Test = %T, want Paren", b.Test)
			}
			return
		}
	}
	t.Fatal("no BindValue found")
}

// TestParse_ForStructure asserts the split of the /*%for*/ directive into ast.ForBlock.For
// (Identifier / Expression) by reaching into the node. The lossless round-trip cannot cover
// this: ForDirective.Text() re-emits the raw token verbatim, so a parse-and-Text() check never
// reads back the split identifier / iterable. Everything after "in" is the iterable expression
// verbatim — including colons (a ternary, a slice, a map), which are no longer special.
func TestParse_ForStructure(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantID   string
		wantExpr string
	}{
		{"basic", "/*%for x in xs*/y/*%end*/", "x", "xs"},
		{"slice colon passes through", "/*%for x in xs[1:2]*/y/*%end*/", "x", "xs[1:2]"},
		{"map colon passes through", "/*%for x in {k: v}*/y/*%end*/", "x", "{k: v}"},
		{"ternary colon passes through", "/*%for x in a ? b : c*/y/*%end*/", "x", "a ? b : c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := parser.Parse(c.src)
			if err != nil {
				t.Fatalf("parser.Parse(%q): %v", c.src, err)
			}
			st, ok := n.(ast.Statement)
			if !ok {
				t.Fatalf("root is %T, want Statement", n)
			}
			var fb *ast.ForBlock
			for _, node := range st.Nodes {
				if b, ok := node.(ast.ForBlock); ok {
					bb := b
					fb = &bb
					break
				}
			}
			if fb == nil {
				t.Fatalf("no ForBlock found in %q", c.src)
			}
			if fb.For.Identifier != c.wantID {
				t.Errorf("For.Identifier = %q, want %q", fb.For.Identifier, c.wantID)
			}
			if fb.For.Expression != c.wantExpr {
				t.Errorf("For.Expression = %q, want %q", fb.For.Expression, c.wantExpr)
			}
		})
	}
}

// TestParse_BindTrailing asserts the "::cast" fold: content after the bind's test literal
// folds into BindValue.Trailing (here `1` is the Word test and `::bigint` the trailing).
func TestParse_BindTrailing(t *testing.T) {
	n, err := parser.Parse("/*a*/1::bigint")
	if err != nil {
		t.Fatal(err)
	}
	st, ok := n.(ast.Statement)
	if !ok {
		t.Fatalf("root is %T, want Statement", n)
	}
	var bind *ast.BindValue
	for _, node := range st.Nodes {
		if b, ok := node.(ast.BindValue); ok {
			bb := b
			bind = &bb
			break
		}
	}
	if bind == nil {
		t.Fatal("no BindValue found")
	}
	w, isWord := bind.Test.(ast.Word)
	if !isWord {
		t.Fatalf("bind Test = %T, want Word", bind.Test)
	}
	if w.Token != "1" {
		t.Errorf("bind Test word = %q, want %q", w.Token, "1")
	}
	if len(bind.Trailing) == 0 {
		t.Fatal("bind Trailing is empty, want the ::cast fold")
	}
	var trailing strings.Builder
	for _, tn := range bind.Trailing {
		trailing.WriteString(tn.Text())
	}
	if got := trailing.String(); got != "::bigint" {
		t.Errorf("bind Trailing text = %q, want %q", got, "::bigint")
	}
}

// TestParse_ErrorMessages asserts distinct error messages by substring. Each case is a
// malformed template whose message pinpoints the specific defect.
func TestParse_ErrorMessages(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"elseif after else", "/*%if a*/x/*%else*/y/*%elseif b*/z/*%end*/", "an elseif directive appears after else"},
		{"second else", "/*%if a*/x/*%else*/y/*%else*/z/*%end*/", "a second else directive is found"},
		{"else without if", "select 1 /*%else*/x/*%end*/", "the corresponding if directive is not found"},
		{"for without identifier", "/*%for in xs*/y/*%end*/", "the identifier is not found in the for directive"},
		{"for without iterable", "/*%for x in */y/*%end*/", "the iterable expression is not found in the for directive"},
		{"empty literal directive", "/*^ */x", "expression is not found in the literal value directive"},
		// Asymmetry vs bind: reducer.go accepts a Word OR Paren test for a bind, but a literal
		// test must be a Word — a Paren is rejected.
		{"literal test must be a Word not Paren", "/*^x*/(1)", "the test value must follow the literal value directive"},
		// A lexer error (an unterminated quoted literal) surfaces through Parse verbatim.
		{"lexer error propagation", "'unterminated", "unterminated quoted literal"},
		// A lexer error inside a parenthesized sub-parse propagates out through the child parser.
		{"lexer error propagation in parens", "('unterminated", "unterminated quoted literal"},
		// A for block whose /*%end*/ never arrives fails when the block is force-reduced at EOF.
		{"for without end", "/*%for x in y*/ z", "the corresponding end directive is not found"},
		// The if directive carries no expression.
		{"if without expression", "/*%if*/x/*%end*/", "expression is not found in the if directive"},
		// The for directive carries no statement at all.
		{"for without statement", "/*%for*/x/*%end*/", "the statement is not found in the for directive"},
		// The elseif directive carries no expression.
		{"elseif without expression", "/*%if a*/x/*%elseif*/y/*%end*/", "expression is not found in the elseif directive"},
		// A bind directive with nothing following it has no test value.
		{"bind without test value", "/*val*/", "the test value must follow the bind value directive"},
		// A literal directive with nothing following it has no test value.
		{"literal without test value", "/*^val*/", "the test value must follow the literal value directive"},
		// A bind whose only follower is another directive (/*%end*/, /*%elseif*/, /*%else*/) is
		// reduce-unwound by that directive: the bind reducer errors for the missing test value.
		{"bind test unwound by end", "/*%if true*/ /*val*/ /*%end*/", "the test value must follow the bind value directive"},
		{"bind test unwound by elseif", "/*%if true*/ /*val*/ /*%elseif y*/ z /*%end*/", "the test value must follow the bind value directive"},
		{"bind test unwound by else", "/*%if true*/ /*val*/ /*%else*/ z /*%end*/", "the test value must follow the bind value directive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parser.Parse(c.src)
			if err == nil {
				t.Fatalf("parser.Parse(%q): expected error", c.src)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("parser.Parse(%q) error = %q, want substring %q", c.src, err.Error(), c.want)
			}
		})
	}
}

// TestParse_ForArbitraryIterable confirms the for directive accepts an arbitrary iterable
// expression verbatim (here a call with a quoted-string argument): the parser does not inspect
// the expression's internals, which are handed to the evaluator at build time. Parse succeeds.
func TestParse_ForArbitraryIterable(t *testing.T) {
	if _, err := parser.Parse("/*%for x in f('a''b') */x/*%end*/"); err != nil {
		t.Fatalf("parser.Parse: unexpected error: %v", err)
	}
}

func TestParse_Errors(t *testing.T) {
	for _, src := range []string{
		"select * from (select 1",         // unbalanced open paren
		"select 1)",                       // stray close paren
		"select 1 /*%if a*/x",             // if without end
		"select 1 /*%end*/",               // end without block
		"select 1 /*%elseif a*/x/*%end*/", // elseif without if
		"select /* */x",                   // empty bind expression
		"select /*%for x*/y/*%end*/",      // for without "in"
		"select /*a*/ from t",             // a space (not a Word/Paren) follows the bind: no test literal
	} {
		if _, err := parser.Parse(src); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}
