package parser

import (
	"strings"
	"testing"
)

var seeds = []string{
	"",
	"select 1",
	"select name from person where name = /*name*/'x' and age > /*age*/0",
	"select 1 from t where /*%if a*/x = 1/*%elseif b*/y = 2/*%else*/z = 3/*%end*/",
	"select /*%for i in xs*//*# i */ /*# i_next_comma */ /*%end*/ from t",
	"select 1 from (select * from t) union select 1 from u",
	"select 1 /** comment */ from t -- line\n where a in /*ids*/(1, 2)",
	"select 1 from t where /*%with u*/a = /*a*/0/*%end*/",
	"group by\torder by for update option (x)",
	"/*> partial */",
}

// FuzzParse asserts the parser never panics and, when it succeeds on input without a
// dropped parser-comment (/*%!), reproduces the input exactly (the lossless invariant).
func FuzzParse(f *testing.F) {
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		node, err := Parse(src)
		if err != nil {
			return // rejecting malformed input is fine; it must not panic
		}
		// Two intended non-lossless cases (matching Komapper): parser-level comments
		// (/*%!) are dropped, and a delimiter (;) terminates the statement, discarding it
		// and anything after it.
		if strings.Contains(src, "/*%!") || strings.Contains(src, ";") {
			return
		}
		if got := node.Text(); got != src {
			t.Fatalf("lossless round-trip failed\n got: %q\nwant: %q", got, src)
		}
	})
}
