package bisql_test

import (
	"errors"
	"fmt"
	"testing/fstest"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
	"github.com/mpyw/bisql/expr"
)

// --- Package-level examples: the concepts explained in the README. ---

// A bind directive /* expr */ becomes a placeholder, replacing the trailing literal (the
// two-way sample value, ignored at build time). The template is valid SQL on its own; bisql
// turns it into a parameterized statement. SQLWithArgs is the values-embedded rendering, for
// review only — never execute it.
func Example() {
	tmpl, err := bisql.Parse("select id from users where name = /*name*/'sample'")
	if err != nil {
		panic(err)
	}
	stmt, err := tmpl.Build(map[string]any{"name": "Alice"})
	if err != nil {
		panic(err)
	}
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where name = ?
	// [Alice]
	// select id from users where name = 'Alice'
}

// /*%if*/ … /*%elseif*/ … /*%else*/ … /*%end*/ renders the first branch whose condition is
// true. The engine removes nothing implicitly, so a dynamic WHERE is anchored with 1 = 1 and
// each branch leads with its own "and". (These branches bind nothing, so only the SQL varies.)
func Example_conditional() {
	const t = "select id from users where 1 = 1 " +
		"/*%if band == 'adult'*/and age >= 18" +
		"/*%elseif band == 'senior'*/and age >= 65" +
		"/*%else*/and age >= 0/*%end*/"
	tmpl, _ := bisql.Parse(t)
	for _, band := range []string{"adult", "senior", "child"} {
		stmt, _ := tmpl.Build(map[string]any{"band": band})
		fmt.Println(stmt.SQL)
	}
	// Output:
	// select id from users where 1 = 1 and age >= 18
	// select id from users where 1 = 1 and age >= 65
	// select id from users where 1 = 1 and age >= 0
}

// /*%for*/ repeats its body with nothing inserted between iterations, so a list is kept two-way
// by anchoring it (here 1 = 0) and having each iteration lead with its own connector — note the
// leading space inside the body so consecutive iterations do not run together.
func Example_iteration() {
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 0" +
			"/*%for kw in keywords*/ or name like /*kw*/'%x%'/*%end*/",
	)
	stmt, _ := tmpl.Build(map[string]any{"keywords": []any{"%a%", "%b%"}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 0 or name like ? or name like ?
	// [%a% %b%]
	// select id from users where 1 = 0 or name like '%a%' or name like '%b%'
}

// /*^ expr */ inlines the value as a formatted SQL literal instead of binding it — for trusted
// values that cannot be parameterized. It is injection-prone. (No bind, so Args is empty.)
func Example_literal() {
	tmpl, _ := bisql.Parse("select id from users limit /*^limit*/10")
	stmt, _ := tmpl.Build(map[string]any{"limit": 50})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	// Output:
	// select id from users limit 50
	// []
}

// A parenthesized test /*ids*/(...) expands an iterable into a placeholder list, so one bind
// covers a whole IN clause.
func Example_inListExpansion() {
	tmpl, _ := bisql.Parse("select id from users where department_id in /*ids*/(0)")
	stmt, _ := tmpl.Build(map[string]any{"ids": []any{1, 2, 3}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where department_id in (?, ?, ?)
	// [1 2 3]
	// select id from users where department_id in (1, 2, 3)
}

// A scalar test binds a slice as ONE array parameter — the form PostgreSQL's = ANY needs — so
// the placeholder count is fixed regardless of the slice length. The values-embedded form shows
// it as a PostgreSQL array literal.
func Example_arrayBind() {
	tmpl, _ := bisql.Parse(
		"select id from users where department_id = ANY(/*ids*/'{}'::int[])",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"ids": []int{1, 2, 3}})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where department_id = ANY($1::int[])
	// [[1 2 3]]
	// select id from users where department_id = ANY('{1,2,3}'::int[])
}

// @include composes a reusable fragment before parsing. Because the directive rides the
// parser-comment channel, the base statement still runs verbatim in a client without it.
func Example_include() {
	ld := bisql.NewRegistryLoader().Register("active_filter", "and status = /*status*/'active'")
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1 /*%! @include active_filter */",
		bisql.WithLoader(ld),
	)
	stmt, _ := tmpl.Build(map[string]any{"status": "active"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and status = ?
	// [active]
	// select id from users where 1 = 1 and status = 'active'
}

// The dialect only changes the placeholder spelling, never the arguments; the same template
// renders under each. (The values-embedded form is dialect-independent here, so only the
// parameterized SQL is shown; see Example_arrayBind for SQLWithArgs.)
func Example_dialects() {
	const t = "select id from users where name = /*name*/'x' and age >= /*age*/0"
	for _, d := range []dialect.Dialect{dialect.MySQL, dialect.PostgreSQL, dialect.Oracle, dialect.SQLServer} {
		tmpl, _ := bisql.Parse(t, bisql.WithDialect(d))
		stmt, _ := tmpl.Build(map[string]any{"name": "Alice", "age": 18})
		fmt.Printf("%s: %s\n", d.Name(), stmt.SQL)
	}
	// Output:
	// mysql: select id from users where name = ? and age >= ?
	// postgresql: select id from users where name = $1 and age >= $2
	// oracle: select id from users where name = :1 and age >= :2
	// sqlserver: select id from users where name = @p1 and age >= @p2
}

// Build accepts a typed struct as well as a map[string]any, so parameters can be passed with
// Go's type checking rather than as untyped map values. Exported fields are matched to bind
// names; an embedded struct's fields are promoted (and also reachable qualified, e.g.
// Filter.Status).
func Example_structParams() {
	type Filter struct {
		Status string
		MinAge int
	}
	type Params struct {
		Filter
		Name string
	}
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1" +
			" and status = /*Status*/'active'" +
			" and age >= /*MinAge*/0" +
			" and name like /*Name*/'%x%'",
	)
	stmt, _ := tmpl.Build(Params{Filter: Filter{Status: "active", MinAge: 18}, Name: "%ali%"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and status = ? and age >= ? and name like ?
	// [active 18 %ali%]
	// select id from users where 1 = 1 and status = 'active' and age >= 18 and name like '%ali%'
}

// --- Function examples. ---

// Parse compiles a template string into a reusable *Template.
func ExampleParse() {
	tmpl, err := bisql.Parse("select id from users where department_id = /*dept*/0")
	if err != nil {
		panic(err)
	}
	stmt, _ := tmpl.Build(map[string]any{"dept": 3})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where department_id = ?
	// [3]
	// select id from users where department_id = 3
}

// ParseFile reads the root template from an fs.FS (for example an embed.FS) and, unless a
// loader is configured, resolves @include fragments from the same fs.FS.
func ExampleParseFile() {
	fsys := fstest.MapFS{
		"users/by_status.sql": {Data: []byte("select id from users where 1 = 1 /*%! @include users/_active.sql */")},
		"users/_active.sql":   {Data: []byte("and status = /*status*/'active'")},
	}
	tmpl, err := bisql.ParseFile(fsys, "users/by_status.sql")
	if err != nil {
		panic(err)
	}
	stmt, _ := tmpl.Build(map[string]any{"status": "active"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and status = ?
	// [active]
	// select id from users where 1 = 1 and status = 'active'
}

// Expand performs only the @include step and returns the resulting template text, leaving every
// other directive (here the /*status*/ bind) intact — so the result is still two-way.
func ExampleExpand() {
	ld := bisql.NewRegistryLoader().Register("active", "and status = /*status*/'active'")
	expanded, err := bisql.Expand(
		"select id from users where 1 = 1 /*%! @include active */",
		bisql.WithLoader(ld),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(expanded)
	// Output:
	// select id from users where 1 = 1 and status = /*status*/'active'
}

// ExpandFile resolves @include from an fs.FS and returns the expanded, still-two-way text —
// useful for committing snapshots or inspecting with EXPLAIN.
func ExampleExpandFile() {
	fsys := fstest.MapFS{
		"report.sql": {Data: []byte("select count(*) from users /*%! @include _scope.sql */")},
		"_scope.sql": {Data: []byte("where status = /*status*/'active'")},
	}
	expanded, err := bisql.ExpandFile(fsys, "report.sql")
	if err != nil {
		panic(err)
	}
	fmt.Println(expanded)
	// Output:
	// select count(*) from users where status = /*status*/'active'
}

// NewParser builds a Parser once; it is immutable and safe for concurrent use, so it is reused
// across every template rather than reconstructed per call.
func ExampleNewParser() {
	p := bisql.NewParser(bisql.WithDialect(dialect.PostgreSQL))

	users, _ := p.Parse("select id from users where id = /*id*/0")
	depts, _ := p.Parse("select id from departments where id = /*id*/0")

	u, _ := users.Build(map[string]any{"id": 1})
	fmt.Println(u.SQL)
	fmt.Println(u.Args)
	fmt.Println(u.SQLWithArgs())

	d, _ := depts.Build(map[string]any{"id": 2})
	fmt.Println(d.SQL)
	fmt.Println(d.Args)
	fmt.Println(d.SQLWithArgs())
	// Output:
	// select id from users where id = $1
	// [1]
	// select id from users where id = 1
	// select id from departments where id = $1
	// [2]
	// select id from departments where id = 2
}

// WithDialect selects the placeholder style (and literal formatting). (Its point is the
// placeholder spelling; the values-embedded form is the same for both, so only SQL is shown.)
func ExampleWithDialect() {
	const t = "select id from users where id = /*id*/0"
	my, _ := bisql.Parse(t, bisql.WithDialect(dialect.MySQL))
	pg, _ := bisql.Parse(t, bisql.WithDialect(dialect.PostgreSQL))

	m, _ := my.Build(map[string]any{"id": 1})
	p, _ := pg.Build(map[string]any{"id": 1})
	fmt.Println(m.SQL)
	fmt.Println(p.SQL)
	// Output:
	// select id from users where id = ?
	// select id from users where id = $1
}

// WithLoader supplies the Loader that resolves @include fragments.
func ExampleWithLoader() {
	ld := bisql.NewRegistryLoader().Register("recent", "and created_at >= /*since*/'2025-01-01'")
	tmpl, _ := bisql.Parse(
		"select id from audit_logs where 1 = 1 /*%! @include recent */",
		bisql.WithLoader(ld),
	)
	stmt, _ := tmpl.Build(map[string]any{"since": "2025-06-01"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from audit_logs where 1 = 1 and created_at >= ?
	// [2025-06-01]
	// select id from audit_logs where 1 = 1 and created_at >= '2025-06-01'
}

// WithStackedLoader consults loaders in order, falling through when one reports the fragment is
// not found. When no loader has it, resolution fails with an error that is ErrNotFound. (The
// fragment here binds nothing, so only the SQL is shown.)
func ExampleWithStackedLoader() {
	base := bisql.NewRegistryLoader().Register("scope", "and status = 'active'")
	// The empty override has no "scope", so the base loader supplies it.
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1 /*%! @include scope */",
		bisql.WithStackedLoader(bisql.NewRegistryLoader(), base),
	)
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)

	_, err := bisql.Parse(
		"select 1 /*%! @include missing */",
		bisql.WithStackedLoader(bisql.NewRegistryLoader()),
	)
	fmt.Println(errors.Is(err, bisql.ErrNotFound))
	// Output:
	// select id from users where 1 = 1 and status = 'active'
	// true
}

// identEvaluator is a minimal expr.Evaluator that resolves an expression as a bare scope key.
type identEvaluator struct{}

func (identEvaluator) Eval(expression string, scope expr.Scope) (any, error) {
	return scope[expression], nil
}

// WithEvaluator replaces the default expr-lang evaluator with a custom one.
func ExampleWithEvaluator() {
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1 /*%if active*/and name = /*name*/'x'/*%end*/",
		bisql.WithEvaluator(identEvaluator{}),
	)
	stmt, _ := tmpl.Build(map[string]any{"active": true, "name": "Alice"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and name = ?
	// [Alice]
	// select id from users where 1 = 1 and name = 'Alice'
}

// NewRegistryLoader holds fragments in memory; Register chains, so several are added fluently.
// (These fragments bind nothing, so only the SQL is shown.)
func ExampleNewRegistryLoader() {
	ld := bisql.NewRegistryLoader().
		Register("active", "and status = 'active'").
		Register("adult", "and age >= 18")
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1 /*%! @include active */ /*%! @include adult */",
		bisql.WithLoader(ld),
	)
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)
	// Output:
	// select id from users where 1 = 1 and status = 'active' and age >= 18
}

// NewFSLoader resolves @include names as file paths in an fs.FS.
func ExampleNewFSLoader() {
	fsys := fstest.MapFS{
		"_scope.sql": {Data: []byte("and status = /*status*/'active'")},
	}
	ld := bisql.NewFSLoader(fsys)
	tmpl, _ := bisql.Parse(
		"select id from users where 1 = 1 /*%! @include _scope.sql */",
		bisql.WithLoader(ld),
	)
	stmt, _ := tmpl.Build(map[string]any{"status": "active"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and status = ?
	// [active]
	// select id from users where 1 = 1 and status = 'active'
}

// NewStackedLoader builds the loader explicitly; WithStackedLoader is a shorthand for
// WithLoader(NewStackedLoader(...)). (The fragment binds nothing, so only the SQL is shown.)
func ExampleNewStackedLoader() {
	env := bisql.NewRegistryLoader() // empty: falls through
	base := bisql.NewRegistryLoader().Register("scope", "and status = 'active'")
	ld := bisql.NewStackedLoader(env, base)

	tmpl, _ := bisql.Parse("select id from users where 1 = 1 /*%! @include scope */", bisql.WithLoader(ld))
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)
	// Output:
	// select id from users where 1 = 1 and status = 'active'
}

// LoaderFunc adapts a plain resolver function to the Loader interface. (The fragment binds
// nothing, so only the SQL is shown.)
func ExampleLoaderFunc() {
	ld := bisql.LoaderFunc(func(name string) (string, error) {
		return "and " + name + " = 1", nil
	})
	tmpl, _ := bisql.Parse("select id from users where 1 = 1 /*%! @include is_active */", bisql.WithLoader(ld))
	stmt, _ := tmpl.Build(nil)
	fmt.Println(stmt.SQL)
	// Output:
	// select id from users where 1 = 1 and is_active = 1
}

// --- Method examples. ---

// Parser.Parse compiles a template string with the parser's configuration.
func ExampleParser_Parse() {
	p := bisql.NewParser()
	tmpl, err := p.Parse("select id from users where name = /*name*/'x'")
	if err != nil {
		panic(err)
	}
	stmt, _ := tmpl.Build(map[string]any{"name": "Alice"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where name = ?
	// [Alice]
	// select id from users where name = 'Alice'
}

// Parser.ParseFile reads the root template from an fs.FS with the parser's configuration.
func ExampleParser_ParseFile() {
	p := bisql.NewParser(bisql.WithDialect(dialect.PostgreSQL))
	fsys := fstest.MapFS{
		"q.sql": {Data: []byte("select id from users where id = /*id*/0")},
	}
	tmpl, err := p.ParseFile(fsys, "q.sql")
	if err != nil {
		panic(err)
	}
	stmt, _ := tmpl.Build(map[string]any{"id": 7})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where id = $1
	// [7]
	// select id from users where id = 7
}

// Template.Build applies parameters, evaluating the directives into (SQL, Args). A parsed
// Template is immutable, so it is built repeatedly with different parameters. (The "without"
// case binds nothing.)
func ExampleTemplate_Build() {
	tmpl, _ := bisql.Parse("select id from users where 1 = 1 /*%if name != null*/and name = /*name*/'x'/*%end*/")

	with, _ := tmpl.Build(map[string]any{"name": "Alice"})
	fmt.Printf("%q %v\n", with.SQL, with.Args)
	fmt.Println(with.SQLWithArgs())

	without, _ := tmpl.Build(map[string]any{})
	fmt.Printf("%q %v\n", without.SQL, without.Args)
	// Output:
	// "select id from users where 1 = 1 and name = ?" [Alice]
	// select id from users where 1 = 1 and name = 'Alice'
	// "select id from users where 1 = 1 " []
}

// RegistryLoader.Register adds a fragment and returns the loader, so calls chain.
func ExampleRegistryLoader_Register() {
	ld := bisql.NewRegistryLoader().Register("scope", "and status = /*status*/'active'")
	tmpl, _ := bisql.Parse("select id from users where 1 = 1 /*%! @include scope */", bisql.WithLoader(ld))
	stmt, _ := tmpl.Build(map[string]any{"status": "banned"})
	fmt.Println(stmt.SQL)
	fmt.Println(stmt.Args)
	fmt.Println(stmt.SQLWithArgs())
	// Output:
	// select id from users where 1 = 1 and status = ?
	// [banned]
	// select id from users where 1 = 1 and status = 'banned'
}

// Statement.SQLWithArgs returns the values-embedded rendering, for review and snapshots only —
// never execute it. It is computed on demand from Args.
func ExampleStatement_SQLWithArgs() {
	tmpl, _ := bisql.Parse(
		"select id from users where name = /*name*/'x' and age >= /*age*/0",
		bisql.WithDialect(dialect.PostgreSQL),
	)
	stmt, _ := tmpl.Build(map[string]any{"name": "Alice", "age": 20})
	fmt.Println(stmt.SQL)           // execute this
	fmt.Println(stmt.Args)          // the bind arguments
	fmt.Println(stmt.SQLWithArgs()) // review only; never execute
	// Output:
	// select id from users where name = $1 and age >= $2
	// [Alice 20]
	// select id from users where name = 'Alice' and age >= 20
}
