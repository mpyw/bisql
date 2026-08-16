package parser

import (
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// The tree is lossless: Text() reproduces the input, except parser comments (/*%!) and a
// trailing delimiter (;) are dropped.
func TestParse_Lossless(t *testing.T) {
	cases := []string{
		"",
		"select 1",
		"select * from t where a = /*a*/'x' and b > /*b*/0",
		"select id from t where id in /*ids*/(1, 2, 3)",
		"select * from (select id from t) x",
		"select 1 from t where /*%if a*/x = 1/*%elseif b*/y = 2/*%else*/z = 3/*%end*/",
		"select /*%for c in cols*/x/*%if c_has_next*/,/*%end*//*%end*/ from t",
		"select 1 where /*%with u*/a = /*a*/0/*%end*/",
		"select /** keep */ /*# also-a-comment */ 1 -- trailing\nfrom t",
		"where a = /*a*/1::bigint and b = ANY(/*b*/'{}'::int[])",
		"select `a`, \"b\", 'c/*x*/' from t",
	}
	for _, src := range cases {
		n, err := Parse(src)
		if err != nil {
			t.Errorf("Parse(%q): %v", src, err)
			continue
		}
		if got := n.Text(); got != src {
			t.Errorf("lossless mismatch\n got: %q\nwant: %q", got, src)
		}
	}
}

func TestParse_ParserCommentAndDelimiterDropped(t *testing.T) {
	n, err := Parse("select 1 /*%! note */ from t; select 2")
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
	n, err := Parse("/*%if p*/y/*%end*/ /*a*/'v'")
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
	n, _ := Parse("id in /*ids*/(1, 2)")
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
		"select /*a*/ from t",             // no test value after bind (space then keyword-word... 'from' is a Word, ok) -> actually a bind needs a Word/Paren test
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}
