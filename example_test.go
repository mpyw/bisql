package bisql_test

import (
	"fmt"
	"testing/fstest"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// A bind directive /* expr */ becomes a placeholder; the literal after it (here 'x') is the
// 2-way test value, ignored at build time but keeping the raw template runnable.
func Example() {
	tmpl, err := bisql.Parse("select name from person where name = /*name*/'x'")
	if err != nil {
		panic(err)
	}
	stmt, err := tmpl.Build(map[string]any{"name": "SCOTT"})
	if err != nil {
		panic(err)
	}
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select name from person where name = ?
	// [SCOTT]
}

// The engine removes nothing implicitly, so a dynamic WHERE anchors with 1 = 1 and each
// condition carries its own leading AND. Absent conditions simply render nothing.
func Example_dynamicWhere() {
	const t = "select * from person where 1 = 1" +
		" /*%if name != null*/and name = /*name*/'x'/*%end*/" +
		" /*%if minAge != null*/and age >= /*minAge*/0/*%end*/"
	tmpl, _ := bisql.Parse(t)

	full, _ := tmpl.Build(map[string]any{"name": "SCOTT", "minAge": 20})
	fmt.Printf("%q %v\n", full.SQL, full.Args)

	none, _ := tmpl.Build(map[string]any{})
	fmt.Printf("%q\n", none.SQL)
	// Output:
	// "select * from person where 1 = 1 and name = ? and age >= ?" [SCOTT 20]
	// "select * from person where 1 = 1  "
}

// A scalar test literal binds a slice as ONE array parameter (Postgres = ANY), avoiding a
// variable number of placeholders.
func Example_arrayBind() {
	tmpl, _ := bisql.Parse(
		"select * from t where id = ANY(/*ids*/'{}'::int[])",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"ids": []int{10, 20, 30}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select * from t where id = ANY($1::int[])
	// [[10 20 30]]
}

// @include composes static fragments as a preprocessing step; Expand returns the resolved,
// still-2-way SQL.
func Example_include() {
	ld := bisql.NewRegistry().Register("active", "/*%if activeOnly*/retired = /*zero*/0/*%end*/")

	tmpl, _ := bisql.Parse("select emp_no from employees where /*%! @include active */ 1 = 1", bisql.WithLoader(ld))
	stmt, _ := tmpl.Build(map[string]any{"activeOnly": true, "zero": 0})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select emp_no from employees where retired = ? 1 = 1
	// [0]
}

// The /*^ */ literal directive inlines a formatted value instead of binding it — for trusted
// values that cannot be parameterized (review output, DDL). It is injection-prone.
func Example_literal() {
	tmpl, _ := bisql.Parse("select * from t limit /*^n*/10")
	stmt, _ := tmpl.Build(map[string]any{"n": 50})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select * from t limit 50
	// []
}

// A /*%for*/ loop with a ': sep' clause emits the separator between iterations only, keeping
// a multi-row VALUES list two-way.
func Example_forLoop() {
	tmpl, _ := bisql.Parse(
		"insert into t (a) values /*%for x in xs : ', '*/(/*x*/0)/*%end*/",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"xs": []any{1, 2, 3}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// insert into t (a) values ($1), ($2), ($3)
	// [1 2 3]
}

// Struct parameters expose exported fields by name; an embedded struct's fields are promoted
// (TenantID) and also reachable qualified (Base.TenantID).
func Example_structParams() {
	type Base struct{ TenantID int }
	type Query struct {
		Base
		Name string
	}
	tmpl, _ := bisql.Parse("where tenant = /*TenantID*/0 and shard = /*Base.TenantID*/0 and name = /*Name*/'x'")
	stmt, _ := tmpl.Build(Query{Base: Base{TenantID: 7}, Name: "SCOTT"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// where tenant = ? and shard = ? and name = ?
	// [7 7 SCOTT]
}

// A StackedLoader tries its loaders in order, falling through when one reports the fragment is
// not found. Here the (empty) override has no "f", so the base registry supplies it.
func Example_stackedLoader() {
	override := bisql.NewRegistry()
	base := bisql.NewRegistry().Register("f", "and base = 1")
	tmpl, _ := bisql.Parse("where 1 = 1 /*%! @include f */", bisql.WithStackedLoader(override, base))
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)
	// Output:
	// where 1 = 1 and base = 1
}

// ExpandFile resolves @include from an fs.FS and returns the expanded, still-two-way text —
// useful for snapshots or EXPLAIN. Fragments resolve from the same fs.FS.
func Example_expandFile() {
	fsys := fstest.MapFS{
		"q.sql":  {Data: []byte("select 1 /*%! @include _f.sql */ from t")},
		"_f.sql": {Data: []byte("where 2 = 2")},
	}
	expanded, _ := bisql.ExpandFile(fsys, "q.sql")
	fmt.Println(expanded)
	// Output:
	// select 1 where 2 = 2 from t
}

// SQLWithArgs returns the values-embedded form, for review and snapshots only — never execute
// it. It is computed on demand from Args.
func ExampleStatement_sqlWithArgs() {
	tmpl, _ := bisql.Parse(
		"where name = /*name*/'x' and age >= /*age*/0",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"name": "SCOTT", "age": 20})
	fmt.Println(stmt.SQL)           // execute this
	fmt.Println(stmt.SQLWithArgs()) // review only; never execute
	// Output:
	// where name = $1 and age >= $2
	// where name = 'SCOTT' and age >= 20
}
