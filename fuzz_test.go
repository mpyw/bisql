package bisql_test

import (
	"testing"

	"github.com/mpyw/bisql"
)

// FuzzBuild asserts the whole pipeline (Parse + Build) never panics on arbitrary input.
// Errors are expected and fine; a panic or hang is not.
func FuzzBuild(f *testing.F) {
	seeds := []string{
		"select 1",
		"select name from t where name = /*name*/'x' and age > /*age*/0",
		"select 1 from t where /*%if a*/x/*%elseif b*/y/*%else*/z/*%end*/",
		"select /*%for i in xs*//*# i */ /*%end*/ from t",
		"select 1 from t where id in /*ids*/(1, 2)",
		"select 1 from t where /*%with u*/a = /*a*/0/*%end*/",
		"select -0 from (select 1) union select /*^v*/1",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	params := map[string]any{
		"name": "x", "age": 1, "a": true, "b": false,
		"xs": []any{1, 2, 3}, "ids": []any{1, 2}, "u": map[string]any{"a": 1}, "v": 1,
	}
	f.Fuzz(func(t *testing.T, src string) {
		tmpl, err := bisql.Parse(src)
		if err != nil {
			return
		}
		_, _ = tmpl.Build(params)
	})
}
