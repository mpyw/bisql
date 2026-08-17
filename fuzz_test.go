package bisql_test

import (
	"testing"

	"github.com/mpyw/bisql"
)

// FuzzBuild asserts the whole pipeline (Parse + Build) never panics on arbitrary input.
func FuzzBuild(f *testing.F) {
	seeds := []string{
		"select 1",
		"select * from t where 1 = 1 /*%if a != null*/and a = /*a*/0/*%end*/",
		"select 1 /*%if a*/x/*%elseif b*/y/*%else*/z/*%end*/",
		"where 1 = 0 /*%for kw in kws*/or x like /*kw*/'y'/*%end*/",
		"where id in /*ids*/(1, 2)",
		"select /*a*/1::int, ANY(/*b*/'{}'::int[])",
		"'{}' \"id\" `x` -- c\n/** c2 */",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	params := map[string]any{
		"a": true, "b": false, "kws": []any{"%a%"}, "ids": []any{1, 2},
		"u": map[string]any{"a": 1},
	}
	f.Fuzz(func(t *testing.T, src string) {
		tmpl, err := bisql.Parse(src)
		if err != nil {
			return
		}
		_, _ = tmpl.Build(params)
	})
}
