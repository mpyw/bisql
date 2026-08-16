package bisql_test

import (
	"sync"
	"testing"

	"github.com/mpyw/bisql"
)

const benchTmpl = `
select emp_no, first_name, last_name
from employees
where 1 = 1
/*%if name != null*/and first_name = /*name*/'x'/*%end*/
/*%if depts != null*/and dept_no in /*depts*/('d001')/*%end*/
order by emp_no`

func BenchmarkBuild(b *testing.B) {
	tmpl, err := bisql.Parse(benchTmpl)
	if err != nil {
		b.Fatal(err)
	}
	params := map[string]any{"name": "Georgi", "depts": []any{"d001", "d002"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tmpl.Build(params); err != nil {
			b.Fatal(err)
		}
	}
}

// A parsed Template is immutable and safe for concurrent Build calls (run with -race).
func TestTemplateConcurrentBuild(t *testing.T) {
	tmpl, err := bisql.Parse(benchTmpl)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				stmt, err := tmpl.Build(map[string]any{"name": "Georgi", "depts": []any{"d001"}})
				if err != nil {
					t.Errorf("build: %v", err)
					return
				}
				if len(stmt.Args) != 2 {
					t.Errorf("got %d args, want 2", len(stmt.Args))
					return
				}
			}
		}()
	}
	wg.Wait()
}
