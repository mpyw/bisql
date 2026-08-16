package parser

import (
	"strings"
	"testing"
)

var seeds = []string{
	"",
	"select 1",
	"select * from t where a = /*a*/'x' and b > /*b*/0",
	"select id from t where id in /*ids*/(1, 2)",
	"select 1 /*%if a*/x/*%elseif b*/y/*%else*/z/*%end*/",
	"select /*%for c in cols*/x/*%if c_has_next*/,/*%end*//*%end*/ from t",
	"select 1 /*%with u*/a = /*a*/0/*%end*/",
	"select 1 /** c */ /*# c2 */ -- line\n from `t`",
	"a = /*a*/1::bigint",
	"'{}' \"id\" `x`",
	"-0 line\xa1#\x9b", // regression: the signed-number spin found earlier
}

// FuzzParse asserts the parser never panics and, when it succeeds on input without a
// dropped parser-comment (/*%!) or delimiter (;), reproduces the input exactly.
func FuzzParse(f *testing.F) {
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		node, err := Parse(src)
		if err != nil {
			return // rejecting malformed input is fine; it must not panic
		}
		if strings.Contains(src, "/*%!") || strings.Contains(src, ";") {
			return // intentionally non-lossless
		}
		if got := node.Text(); got != src {
			t.Fatalf("lossless round-trip failed\n got: %q\nwant: %q", got, src)
		}
	})
}
