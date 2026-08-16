package bisql_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/pkg/expr"
)

// keyEvaluator is a trivial custom evaluator: it treats each expression as a scope key.
// It shares no code with the default expr-lang evaluator, so exercising it proves
// WithEvaluator actually swaps the evaluator.
type keyEvaluator struct{ calls int }

func (e *keyEvaluator) Eval(expression string, scope expr.Scope) (any, error) {
	e.calls++
	return scope[expression], nil
}

func TestWithEvaluator(t *testing.T) {
	ev := &keyEvaluator{}
	tmpl, err := bisql.Parse(
		"select 1 where /*%if flag*/a = /*val*/0/*%end*/",
		bisql.WithEvaluator(ev),
	)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"flag": true, "val": 7})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "select 1 where a = ?" {
		t.Errorf("SQL got %q", stmt.SQL)
	}
	if !reflect.DeepEqual(stmt.Args, []any{7}) {
		t.Errorf("Args got %#v", stmt.Args)
	}
	if ev.calls == 0 {
		t.Error("custom evaluator was never called")
	}
}

func TestLoadFS(t *testing.T) {
	fsys := fstest.MapFS{
		"sql/active.sql": &fstest.MapFile{
			Data: []byte(`/*%if activeOnly*/retired = /*zero*/0/*%end*/`),
		},
	}
	ld := bisql.NewLoader()
	if err := ld.LoadFS(fsys, "sql/*.sql"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	// The fragment is registered under its path without extension: "sql/active".
	tmpl, err := ld.Parse("select emp_no from employees where /*> sql/active */")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tmpl.Build(map[string]any{"activeOnly": true, "zero": 0})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "select emp_no from employees where retired = ?" {
		t.Errorf("SQL got %q", stmt.SQL)
	}
	if !reflect.DeepEqual(stmt.Args, []any{0}) {
		t.Errorf("Args got %#v", stmt.Args)
	}
}

// Clause keywords beyond WHERE, and set operations, pass through and take part in
// clause-removal. Exercises the lexer's multi-word keyword matchers.
func TestClausesAndSetOps(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "group by / having pass through",
			tmpl:   "select dept, count(*) from emp group by dept having count(*) > 1",
			params: nil,
			sql:    "select dept, count(*) from emp group by dept having count(*) > 1",
		},
		{
			name:   "for update always kept",
			tmpl:   "select 1 from t for update",
			params: nil,
			sql:    "select 1 from t for update",
		},
		{
			name:   "option clause",
			tmpl:   "select 1 from t option (recompile)",
			params: nil,
			sql:    "select 1 from t option (recompile)",
		},
		{
			name:   "having removed when empty",
			tmpl:   "select dept from emp group by dept having /*%if f*/count(*) > /*n*/1/*%end*/",
			params: map[string]any{"f": false},
			sql:    "select dept from emp group by dept ",
		},
		{
			name:   "except",
			tmpl:   "select a from t1 except select a from t2",
			params: nil,
			sql:    "select a from t1 except select a from t2",
		},
		{
			name:   "intersect",
			tmpl:   "select a from t1 intersect select a from t2",
			params: nil,
			sql:    "select a from t1 intersect select a from t2",
		},
		{
			name:   "minus",
			tmpl:   "select a from t1 minus select a from t2",
			params: nil,
			sql:    "select a from t1 minus select a from t2",
		},
	})
}

// In a set operation, an operand that renders empty drops the operand (and its keyword).
func TestSetOpEmptyOperand(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "right operand present",
			tmpl:   "select a from t1 union /*%if more*/select a from t2/*%end*/",
			params: map[string]any{"more": true},
			sql:    "select a from t1 union select a from t2",
		},
		{
			name:   "right operand empty drops union",
			tmpl:   "select a from t1 union /*%if more*/select a from t2/*%end*/",
			params: map[string]any{"more": false},
			sql:    "select a from t1 ",
		},
	})
}
