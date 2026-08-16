package parser

import (
	"testing"

	"github.com/mpyw/bisql/internal/sqltmpl/ast"
)

// roundtrip asserts Parse(src).Text() == src (lossless), the parser's core invariant.
func roundtrip(t *testing.T, src string) ast.Node {
	t.Helper()
	n, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	if got := n.Text(); got != src {
		t.Fatalf("round-trip\n src: %q\n got: %q", src, got)
	}
	return n
}

func mustErr(t *testing.T, src string) {
	t.Helper()
	if _, err := Parse(src); err == nil {
		t.Fatalf("Parse(%q): expected error", src)
	}
}

// Cases ported from Komapper's SqlParserTest (lossless round-trip focus).

func TestParse_empty(t *testing.T) {
	n := roundtrip(t, "")
	if st, ok := n.(ast.Statement); !ok || len(st.Nodes) != 0 {
		t.Fatalf("empty must be an empty Statement, got %#v", n)
	}
}

func TestParse_roundtrip(t *testing.T) {
	cases := []string{
		"select * from person",
		"select name, age from person where name = /*name*/'test' and age > 1 order by name, age for update",
		"select name, age",
		"from person inner join salary",
		"where name = 'aaa'",
		"group by name",
		"having count(*) > 1",
		"order by name, age",
		"and age > 1",
		"or age > 1",
		"/* age */1",
		"/* age */(1,2,3)",
		"/*# age */",
		"/*^ age */1",
		"-- single line comment",
		"select * from person union select * from employee",
		"select * from person union select * from employee union select * from worker",
		"select date()",
		"select * from (select * from person)",
		"/*%if a*/ b /*%end*/ h",
		"/*% if a */ b /*% end */ h",
		"/*%if a*/ b /*%elseif c*/ d /*%end*/ h",
		"/*%if a*/ b /*%elseif c*/ d /*%elseif e*/ f /*%else*/ g /*%end*/ h",
		"/*%if a*/ b /*%else*/ d /*%end*/ h",
		"/*%if aaa*/ a /*%for bbb in ccc*/ b /*%end*/ c /*%end*/",
		"/*%if aaa*/ a /*%if bbb*/ b /*%end*/ c /*%end*/",
		"/*%for a in aaa*/ b /*%end*/ h",
		"/*% for a in aaa */ b /*% end */ h",
		"/*%for a in aaa*/ b /*%for c in ccc*/ d /*%end*/ e /*%end*/",
		"/*%with a*/ b /*%end*/ h",
		"select emp_no from employees where /*> active */",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) { roundtrip(t, c) })
	}
}

func TestParse_parserLevelCommentIsDropped(t *testing.T) {
	n, err := Parse("select a/*%! comment */ from b")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Text(); got != "select a from b" {
		t.Errorf("got %q, want %q", got, "select a from b")
	}
}

func TestParse_structures(t *testing.T) {
	// bind value: test value is a Word
	n, _ := Parse("/* age */1")
	st := n.(ast.Statement)
	bv := st.Nodes[0].(ast.BindValue)
	if _, ok := bv.Test.(ast.Word); !ok {
		t.Errorf("bind test value: want Word, got %T", bv.Test)
	}
	// bind value: test value is a Paren
	n, _ = Parse("/* age */(1,2,3)")
	bv = n.(ast.Statement).Nodes[0].(ast.BindValue)
	if _, ok := bv.Test.(ast.Paren); !ok {
		t.Errorf("bind test value: want Paren, got %T", bv.Test)
	}
	// set: left and right are statements
	n, _ = Parse("select * from a union select * from b")
	set := n.(ast.Set)
	if _, ok := set.Left.(ast.Statement); !ok {
		t.Errorf("set.Left: want Statement, got %T", set.Left)
	}
	if _, ok := set.Right.(ast.Statement); !ok {
		t.Errorf("set.Right: want Statement, got %T", set.Right)
	}
	// partial name
	n, _ = Parse("/*> orderBy */")
	p := n.(ast.Statement).Nodes[0].(ast.Partial)
	if p.Name != "orderBy" {
		t.Errorf("partial name: got %q", p.Name)
	}
}

func TestParse_forDirectiveSplit(t *testing.T) {
	cases := []struct{ src, id, expr string }{
		{"/*%for item in items*/ b /*%end*/", "item", "items"},
		{"/*%for index in indexes*/ b /*%end*/", "index", "indexes"},
		{"/*%for line in lines*/ b /*%end*/", "line", "lines"},
		{"/*%for x in xs.filter { it in ys }*/ b /*%end*/", "x", "xs.filter { it in ys }"},
	}
	for _, c := range cases {
		n, err := Parse(c.src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		fb := n.(ast.Statement).Nodes[0].(ast.ForBlock)
		if fb.For.Identifier != c.id || fb.For.Expression != c.expr {
			t.Errorf("for %q: id=%q expr=%q, want id=%q expr=%q", c.src, fb.For.Identifier, fb.For.Expression, c.id, c.expr)
		}
	}
}

func TestParse_errors(t *testing.T) {
	for _, src := range []string{
		"/* */",             // empty bind expression
		"/*# */",            // empty embedded expression
		"/*^ */",            // empty literal expression
		"/* aaa */",         // test value must follow bind
		"/*^ aaa */",        // test value must follow literal
		"select date(",      // close paren not found
		"/*%if aaa*/ a",     // missing end
		"/*%elseif aaa*/ a", // elseif without if
		"/*%else*/ a",       // else without if
		"/*%end*/ a",        // end without block
		"/*%if */",          // empty if expression
		"/*%elseif */",      // empty elseif expression
		"/*%if aaa*/ b /*%else*/ c /*%elseif d*/ e /*%end*/", // elseif after else
		"/*%if aaa*/ b /*%else*/ c /*%else*/ d /*%end*/",     // double else
		"/*%for a in aaa*/ b",                                // for missing end
		"/*%for */",                                          // for empty statement
	} {
		t.Run(src, func(t *testing.T) { mustErr(t, src) })
	}
}
