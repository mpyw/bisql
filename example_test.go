package bisql_test

import (
	"fmt"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// The bind directive /* expr */ becomes a placeholder; the literal after it (here 'x') is
// the 2-way test value, ignored at build time but keeping the raw template runnable.
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

// An /*%if*/ whose condition is false drops its content, and the empty WHERE clause is
// removed along with any dangling connector.
func Example_conditionalClauseRemoval() {
	const t = "select name from person where /*%if name != null*/name = /*name*/'x'/*%end*/ order by name"
	tmpl, _ := bisql.Parse(t)

	with, _ := tmpl.Build(map[string]any{"name": "SCOTT"})
	fmt.Printf("%q\n", with.SQL)

	without, _ := tmpl.Build(map[string]any{"name": nil})
	fmt.Printf("%q\n", without.SQL)
	// Output:
	// "select name from person where name = ? order by name"
	// "select name from person   order by name"
}

// Partials factor a template into named, reusable fragments. Unlike Komapper's raw embed,
// the fragment is re-parsed, so /*%if*/ and binds inside it work — recursively.
func Example_partial() {
	ld := bisql.NewLoader()
	ld.Register("active", `/*%if activeOnly*/retired = /*zero*/0/*%end*/`)

	tmpl, err := ld.Parse("select emp_no from employees where /*> active */")
	if err != nil {
		panic(err)
	}
	stmt, _ := tmpl.Build(map[string]any{"activeOnly": true, "zero": 0})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select emp_no from employees where retired = ?
	// [0]
}

// WithDialect selects placeholder style; a list bind expands to an IN list.
func Example_dialect() {
	tmpl, _ := bisql.Parse(
		"select name from person where id in /*ids*/(1, 2)",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"ids": []any{10, 20, 30}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select name from person where id in ($1, $2, $3)
	// [10 20 30]
}
