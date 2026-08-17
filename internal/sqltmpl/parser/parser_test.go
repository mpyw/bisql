package parser_test

import (
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
		// for-loop with a `: 'sep'` separator clause (build-only; the directive is a comment
		// when pasted raw, so the raw text is a single-element list `select 0 id from t`).
		"select /*%for c in cols : ', '*//*c*/0/*%end*/ id from t",
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
	n, _ := parser.Parse("id in /*ids*/(1, 2)")
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
