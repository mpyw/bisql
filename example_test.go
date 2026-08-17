package bisql_test

import (
	"fmt"

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
