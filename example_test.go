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
	tmpl, err := bisql.Parse("select name from users where name = /*name*/'x'")
	if err != nil {
		panic(err)
	}
	stmt, err := tmpl.Build(map[string]any{"name": "Alice"})
	if err != nil {
		panic(err)
	}
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select name from users where name = ?
	// [Alice]
}

// The engine removes nothing implicitly, so a dynamic WHERE anchors with 1 = 1 and each
// condition carries its own leading AND. Absent conditions simply render nothing.
func Example_dynamicWhere() {
	const t = "select * from users where 1 = 1" +
		" /*%if name != null*/and name = /*name*/'x'/*%end*/" +
		" /*%if minAge != null*/and age >= /*minAge*/0/*%end*/"
	tmpl, _ := bisql.Parse(t)

	full, _ := tmpl.Build(map[string]any{"name": "Bob", "minAge": 18})
	fmt.Printf("%q %v\n", full.SQL, full.Args)

	none, _ := tmpl.Build(map[string]any{})
	fmt.Printf("%q\n", none.SQL)
	// Output:
	// "select * from users where 1 = 1 and name = ? and age >= ?" [Bob 18]
	// "select * from users where 1 = 1  "
}

// A scalar test literal binds a slice as ONE array parameter (Postgres = ANY), avoiding a
// variable number of placeholders.
func Example_arrayBind() {
	tmpl, _ := bisql.Parse(
		"select * from users where id = ANY(/*ids*/'{}'::int[])",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"ids": []int{1, 2, 3}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select * from users where id = ANY($1::int[])
	// [[1 2 3]]
}

// @include composes static fragments as a preprocessing step; Expand returns the resolved,
// still-2-way SQL.
func Example_include() {
	ld := bisql.NewRegistryLoader().Register("active", "/*%if activeOnly*/status = /*status*/'active'/*%end*/")

	tmpl, _ := bisql.Parse("select id from users where /*%! @include active */ 1 = 1", bisql.WithLoader(ld))
	stmt, _ := tmpl.Build(map[string]any{"activeOnly": true, "status": "active"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select id from users where status = ? 1 = 1
	// [active]
}

// The /*^ */ literal directive inlines a formatted value instead of binding it — for trusted
// values that cannot be parameterized (review output, DDL). It is injection-prone.
func Example_literal() {
	tmpl, _ := bisql.Parse("select * from users limit /*^n*/10")
	stmt, _ := tmpl.Build(map[string]any{"n": 50})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select * from users limit 50
	// []
}

// A /*%for*/ loop inserts nothing between iterations; a list is kept two-way by anchoring it
// and having each iteration lead with its own connector. A multi-row insert therefore uses the
// empty-safe INSERT ... SELECT form: a WHERE 1 = 0 seed (zero rows) plus one "union all select"
// per element. An empty list renders just the seed, which inserts nothing.
func Example_forLoop() {
	tmpl, _ := bisql.Parse(
		"insert into audit_logs (user_id) select 0 where 1 = 0"+
			"/*%for id in ids*/ union all select /*id*/0/*%end*/",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"ids": []any{1, 2, 3}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// insert into audit_logs (user_id) select 0 where 1 = 0 union all select $1 union all select $2 union all select $3
	// [1 2 3]
}

// Struct parameters expose exported fields by name; an embedded struct's fields are promoted
// (DepartmentID) and also reachable qualified (Base.DepartmentID).
func Example_structParams() {
	type Base struct{ DepartmentID int }
	type User struct {
		Base
		Name string
	}
	tmpl, _ := bisql.Parse("where dept = /*DepartmentID*/0 and dept_alias = /*Base.DepartmentID*/0 and name = /*Name*/'x'")
	stmt, _ := tmpl.Build(User{Base: Base{DepartmentID: 3}, Name: "Alice"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// where dept = ? and dept_alias = ? and name = ?
	// [3 3 Alice]
}

// A StackedLoader tries its loaders in order, falling through when one reports the fragment is
// not found. Here the (empty) override has no "active", so the base registry supplies it.
func Example_stackedLoader() {
	override := bisql.NewRegistryLoader()
	base := bisql.NewRegistryLoader().Register("active", "and status = 'active'")
	tmpl, _ := bisql.Parse("select id from users where 1 = 1 /*%! @include active */", bisql.WithStackedLoader(override, base))
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)
	// Output:
	// select id from users where 1 = 1 and status = 'active'
}

// ExpandFile resolves @include from an fs.FS and returns the expanded, still-two-way text —
// useful for snapshots or EXPLAIN. Fragments resolve from the same fs.FS.
func Example_expandFile() {
	fsys := fstest.MapFS{
		"users.sql":   {Data: []byte("select id /*%! @include _active.sql */ from users")},
		"_active.sql": {Data: []byte("where status = 'active'")},
	}
	expanded, _ := bisql.ExpandFile(fsys, "users.sql")
	fmt.Println(expanded)
	// Output:
	// select id where status = 'active' from users
}

// SQLWithArgs returns the values-embedded form, for review and snapshots only — never execute
// it. It is computed on demand from Args.
func ExampleStatement_sqlWithArgs() {
	tmpl, _ := bisql.Parse(
		"where name = /*name*/'x' and age >= /*age*/0",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"name": "Alice", "age": 20})
	fmt.Println(stmt.SQL)           // execute this
	fmt.Println(stmt.SQLWithArgs()) // review only; never execute
	// Output:
	// where name = $1 and age >= $2
	// where name = 'Alice' and age >= 20
}
