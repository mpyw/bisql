# bisql

**A two-way SQL template engine for Go.** Directives are expressed as SQL comments, so a
template remains executable without modification in a SQL client, while an application
converts the same text into a parameterized statement `(SQL, Args)`. The directive syntax is
inspired by [Komapper](https://www.komapper.org/)'s TEMPLATE API; the runtime semantics are
defined by bisql's **explicit model**, described in [The explicit model](#the-explicit-model).

## Synopsis

Templates are ordinary `.sql` files, and reusable fragments are separate `.sql` files
composed with `@include`. A template file is loaded with `ParseFile` from any `fs.FS` — for
example an embedded file system — and included fragments are resolved from the same file
system.

```text
sql/
└── employees/
    ├── search.sql     root template
    └── _active.sql    reusable fragment
```

```sql
-- sql/employees/search.sql
select emp_no, name
from employees
where 1 = 1
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
/*%! @include employees/_active.sql */
order by emp_no
```

```sql
-- sql/employees/_active.sql
/*%if activeOnly*/and retired = /*zero*/0/*%end*/
```

```go
package main

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

//go:embed sql
var sqlFiles embed.FS

func main() {
	sqlFS, _ := fs.Sub(sqlFiles, "sql")

	// Configure a Parser once and reuse it for every template.
	p := bisql.NewParser(bisql.WithDialect(dialect.PostgreSQL))

	tmpl, err := p.ParseFile(sqlFS, "employees/search.sql")
	if err != nil {
		panic(err)
	}

	stmt, err := tmpl.Build(map[string]any{
		"name":       "SCOTT",
		"activeOnly": true,
		"zero":       0,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(stmt.SQL)  // parameterized form; execute this
	fmt.Println(stmt.Args) // []any{"SCOTT", 0}
}
```

The resulting statement is:

```sql
select emp_no, name
from employees
where 1 = 1
and name = $1
and retired = $2
order by emp_no
```

A `Parser` is immutable and safe for concurrent use, so it is constructed once and reused
across templates, as above. A template that is available as a string, rather than a file, is
parsed with `p.Parse` instead of `p.ParseFile`; the top-level `bisql.Parse`, `bisql.ParseFile`,
`bisql.Expand`, and `bisql.ExpandFile` functions are shortcuts that construct a single-use
parser from the given options.

A `Statement` exposes the following members:

| Member            | Type     | Description                                                                       |
|:------------------|:---------|:----------------------------------------------------------------------------------|
| `SQL`             | `string` | Parameterized statement intended for execution.                                   |
| `Args`            | `[]any`  | Bind arguments, in placeholder order.                                             |
| `SQLWithArgs()`   | `string` | Values-embedded rendering for review and snapshots. **Must not be executed.**    |

> [!WARNING]
> `SQLWithArgs()` inlines arguments as SQL literals and is provided solely for inspection
> (logging, golden files, `EXPLAIN`). It is not a substitute for parameter binding and must
> never be sent to a database.

## Two-way SQL

Because every directive is written as a SQL comment, a template is simultaneously a valid SQL
statement. Consider the following template:

```sql
select emp_no, name from employees
where 1 = 1
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
order by emp_no
```

When the text is executed verbatim in a SQL client, `/*%if*/`, `/*%end*/`, and `/*name*/` are
interpreted as comments, and the trailing literal `'SCOTT'` remains in place; the client
therefore evaluates `name = 'SCOTT'`. When the same text is processed by bisql, the `/*%if*/`
condition is evaluated, the fragment `/*name*/'SCOTT'` is replaced by a placeholder, and the
value is bound as an argument. The two interpretations are designed to remain semantically
consistent.

## The explicit model

bisql **performs no implicit removal.** The renderer emits the template verbatim; it only
evaluates directives (bind, literal, conditional, iteration) and strips parser comments. In
particular, the engine does not remove empty clauses, does not delete dangling `AND`/`OR`
connectors, and does not normalize whitespace.

This design makes the output a deterministic function of the template text and the evaluated
branches, at the cost of a single authoring obligation: **the author anchors every dynamic
fragment** so that no separator is ever left dangling. The `1 = 1` above serves this purpose.
The complete set of obligations is specified in [Authoring rules](#authoring-rules).

## Directives

### SQL directives

These directives are evaluated during rendering and produce the SQL and its arguments.

<table>
<thead>
<tr><th>Syntax</th><th>Meaning</th></tr>
</thead>
<tbody>
<tr>
<td>

```sql
/* expr */literal
```

</td>
<td>

**Bind.** Emits a placeholder and binds the value of `expr` as an argument, replacing the
trailing `literal` (the two-way sample value, ignored at build time).

```sql
where name = /*name*/'SCOTT'

-- name = "SCOTT"
--   →  where name = $1
--      ($1 = "SCOTT")
```

</td>
</tr>
<tr>
<td>

```sql
/*^ expr */literal
```

</td>
<td>

**Literal.** Inlines the value of `expr` as a formatted SQL literal, replacing the trailing
`literal`, instead of binding it. For trusted values that cannot be parameterized;
injection-prone.

```sql
limit /*^n*/10

-- n = 50
--   →  limit 50
```

</td>
</tr>
<tr>
<td>

```sql
/*%if e*/
    -- body A
/*%elseif e*/
    -- body B
/*%else*/
    -- body C
/*%end*/
```

</td>
<td>

**Conditional.** Renders the first branch whose condition is true, or the `/*%else*/`
branch if none is; the other branches are omitted.

```sql
where 1 = 1 /*%if minAge != null*/and age >= /*minAge*/0/*%end*/

-- minAge = 20
--   →  where 1 = 1 and age >= $1
--      ($1 = 20)
-- minAge = nil
--   →  where 1 = 1
```

</td>
</tr>
<tr>
<td>

```sql
/*%for x in xs*/
    -- body, repeated for each x
/*%end*/
```

An optional separator (typically a comma):

```sql
/*%for x in xs : ','*/
    -- body
/*%end*/
```

</td>
<td>

**Iteration.** Renders the body once for each element of `xs`, bound to `x`. The separator
clause keeps an anchorless list (a multi-row `VALUES`) two-way.

```sql
insert into t (a) values /*%for x in xs : ', '*/(/*x*/0)/*%end*/

-- xs = [1, 2]
--   →  insert into t (a) values ($1), ($2)
--      ($1 = 1, $2 = 2)
```

</td>
</tr>
</tbody>
</table>

### Preprocessor directives

These directives share the `/*%! … */` channel and are resolved before lexing.

<table>
<thead>
<tr><th>Syntax</th><th>Meaning</th></tr>
</thead>
<tbody>
<tr>
<td>

```sql
/*%! @include name */
```

</td>
<td>

**Include.** Splices the named fragment's text in place before lexing (see
[Fragment inclusion](#fragment-inclusion)).

```sql
where 1 = 1 /*%! @include filters/active.sql */
-- → where 1 = 1 <text of filters/active.sql>
```

</td>
</tr>
<tr>
<td>

```sql
/*%! … */
```

</td>
<td>

**Parser comment.** Removed from the output entirely; carries no SQL.

```sql
select 1 /*%! TODO: drop this column */ from t
-- → select 1  from t
```

</td>
</tr>
</tbody>
</table>

All other comment forms — block comments (`/** … */`), line comments (`-- …`), and optimizer
hints (`/*+ … */`) — pass through to the output unchanged.

### Conditionals and iteration

A `/*%for*/` may declare a separator with a trailing `: 'sep'` clause. The separator is
emitted **between iterations only** — never before the first element or after the last — and
solely at build time. Because the `/*%for … : 'sep'*/` directive is a comment when the raw
template is pasted into a client, the raw text contains a single body with no separator. This
is what keeps a list with no anchor position — a multi-row `VALUES` clause, a function-argument
list — two-way:

```sql
insert into audit (emp_no, action)
values /*%for e in entries : ', '*/
  (/*e.empNo*/0, /*e.action*/'x')
/*%end*/
```

| Context               | Result                                     |
|:----------------------|:-------------------------------------------|
| Build (two entries)   | `values ($1, $2), ($3, $4)`                |
| Raw paste in a client | `values (0, 'x')` — a valid single-row `VALUES` |

The separator value is a single-quoted string literal, with `''` for a literal quote.

> [!NOTE]
> A `VALUES` list built this way must have at least one row (an empty list renders `values`
> with nothing after it). For a list that may be empty, use `INSERT … SELECT` with a zero-row
> anchor instead (see [Anchoring dynamic fragments](#anchoring-dynamic-fragments)).

> [!NOTE]
> `/*%for*/` iterates over **values**, which are bound as parameters. It does not emit column
> names or other identifiers; those cannot be parameterized and must be selected with a
> `/*%if*/` whitelist (see [Dynamic identifiers](#dynamic-identifiers)).

### Bind values and IN-list expansion

The **shape of the test literal** determines how a bind expands. This distinction is resolved
from the template text, independently of the runtime value type. The examples below use the
PostgreSQL dialect for their placeholder form.

| Test literal shape | Behavior                                                                          |
|:-------------------|:----------------------------------------------------------------------------------|
| `(...)` (parenthesized) | The value is expanded into a parenthesized placeholder list.                 |
| scalar             | The value is bound as a single parameter, without expansion.                     |

A parenthesized test expands an iterable element-wise:

```sql
-- template
where id in /*ids*/(1, 2)
```

```go
tmpl.Build(map[string]any{"ids": []int{10, 20, 30}})
// SQL:  where id in ($1, $2, $3)
// Args: []any{10, 20, 30}
```

An empty slice renders as `(null)`, and a slice of slices renders as row tuples,
`(($1, $2), ($3, $4))`.

A scalar test binds the value as a single parameter. When the value is a slice, it is bound as
one array parameter, which is the form required by the PostgreSQL `= ANY(...)` construct:

```sql
-- template
where id = ANY(/*ids*/'{}'::int[])
```

```go
tmpl.Build(map[string]any{"ids": []int{10, 20, 30}})
// SQL:  where id = ANY($1::int[])
// Args: []any{[]int{10, 20, 30}}
```

> [!NOTE]
> Array binding is supported only where the dialect and driver provide a native array type;
> in practice this applies to PostgreSQL. See [Dialect portability](#dialect-portability).

### Literal interpolation

The `/*^ expr */` directive inlines the evaluated value directly into `SQL` as a formatted
literal, rather than binding it. It is intended for values that cannot be parameterized (for
example, DDL fragments) and for review output.

```sql
where status = /*^status*/'active'
```

> [!WARNING]
> `/*^ */` performs no parameterization and is therefore susceptible to SQL injection if the
> value originates from untrusted input. Restrict its use to trusted, developer-controlled
> values.

## Fragment inclusion

`@include` is a **preprocessing** step: `/*%! @include name */` is replaced with the text of
the named fragment before lexing. Inclusion is recursive and is guarded against cycles and
unbounded depth. Because the directive is carried on the parser-comment channel, a template
that references a fragment still executes verbatim in a client (the base statement runs
without the fragment).

Fragment resolution is delegated to an implementation of the `Loader` interface:

```go
type Loader interface {
	Load(name string) (string, error)
}
```

Fragment resolution is delegated to an implementation of the `Loader` interface. Four
implementations are provided. There is **no default**; a template that uses `@include` must be
parsed with a loader.

| Implementation   | Constructor                        | Source                                              |
|:-----------------|:-----------------------------------|:----------------------------------------------------|
| `RegistryLoader` | `bisql.NewRegistry()`              | In-memory fragments registered by name.             |
| `FSLoader`       | `bisql.NewFSLoader(fs.FS)`         | Files in an `fs.FS` (`embed.FS`, `os.DirFS`, …); the `@include` name is the file path from the root of the `fs.FS`. |
| `LoaderFunc`     | `bisql.LoaderFunc(fn)`             | An adapter over any resolution function.            |
| `StackedLoader`  | `bisql.NewStackedLoader(…Loader)`  | A chain of loaders tried in order (see below).      |

`ParseFile` (see [Synopsis](#synopsis)) is the file-oriented entry point: it reads the root
template from an `fs.FS` and, unless a loader is configured explicitly, resolves `@include`
fragments from the same `fs.FS`. It is therefore equivalent to the following, which is the
form to use when the root template is a string rather than a file:

```go
loader := bisql.NewFSLoader(sqlFS)
tmpl, err := bisql.Parse(rootSrc, bisql.WithLoader(loader))
```

Fragments may also be provided in memory rather than as files:

```go
loader := bisql.NewRegistry().
	Register("active_filter", "/*%if activeOnly*/and retired = /*zero*/0/*%end*/")

tmpl, err := bisql.Parse(
	"select emp_no from employees where 1 = 1 /*%! @include active_filter */",
	bisql.WithLoader(loader),
)
```

Loaders may be layered with `StackedLoader` (or the `WithStackedLoader` shorthand). Each
loader is tried in order; the chain falls through to the next loader when the current one
reports that the fragment is not found, and aborts immediately on any other error. This
allows, for example, a set of overrides to take precedence over an embedded default set:

```go
tmpl, err := bisql.Parse(src, bisql.WithStackedLoader(
	overrides,                  // consulted first; e.g. a RegistryLoader
	bisql.NewFSLoader(sqlFS),   // fallback: the embedded defaults
))
```

A loader signals "not found" — as opposed to a genuine failure — by returning an error that
satisfies `errors.Is(err, bisql.ErrNotFound)`. `RegistryLoader` does so for an unregistered
name, and `FSLoader` reports a missing file with `fs.ErrNotExist`, which the chain also treats
as "not found". If no loader in the chain has the fragment, resolution fails with an error
satisfying `ErrNotFound`.

### Expanding includes for inspection

The `Expand` and `ExpandFile` functions perform only the inclusion step: they resolve every
`@include` and return the resulting template text, leaving all other directives intact. The
result is therefore still a valid two-way template, which is useful for committing expanded
snapshots and for pre-execution inspection with `EXPLAIN`.

```go
expanded, err := bisql.ExpandFile(sqlFS, "employees/search.sql")
```

## Authoring rules

Because the engine removes nothing implicitly, a template author observes the following rules.

### Anchoring dynamic fragments

Each dynamic construct is preceded by a fixed anchor so that a rendered separator is never
left dangling.

| Construct         | Anchoring rule                                                                          |
|:------------------|:----------------------------------------------------------------------------------------|
| `WHERE` / `HAVING`| Introduce a constant predicate (`1 = 1` for `AND` chains, `1 = 0` for `OR` chains); each condition carries a leading `and`/`or`. |
| `ORDER BY`        | Terminate the list with a stable key (for example, `id`).                               |
| `SELECT` / `SET` column list | Begin with a fixed column and add each optional, whitelisted column with a leading comma inside a `/*%if*/`. |
| List via `/*%for*/` | Declare the separator with the `: 'sep'` clause; it is emitted between iterations only, so no dangling separator remains and no anchor is required. |
| Empty-safe row list | A `VALUES` list built with `: ', '` must be non-empty; for a possibly-empty list, use `INSERT … SELECT` anchored by a zero-row `select … where 1 = 0` and `union all`. |
| `JOIN` / `UNION`  | Place the connector inside the `/*%if*/` block so that each fragment is self-contained. |

```sql
-- WHERE: constant predicate + leading connectors
select * from employees
where 1 = 1
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
/*%if minAge != null*/and age >= /*minAge*/20/*%end*/

-- ORDER BY: trailing stable key
select * from employees
order by /*%if byName*/name, /*%end*/emp_no

-- SELECT list: fixed leading column; optional known columns added with a leading comma.
-- Columns are whitelisted with /*%if*/, never bound.
select emp_no /*%if withName*/, name/*%end*/ /*%if withDept*/, dept_no/*%end*/
from employees

-- Row list: the : ', ' separator is emitted between iterations only (build-only, two-way).
insert into audit (emp_no, action)
values /*%for e in entries : ', '*/(/*e.empNo*/0, /*e.action*/'x')/*%end*/

-- Empty-safe variant: INSERT ... SELECT anchored by a zero-row select, for a list that may
-- have no rows (a VALUES list would render an invalid empty `values`).
insert into audit (emp_no, action)
select 0, '' where 1 = 0
/*%for e in entries : ' '*/union all select /*e.empNo*/0, /*e.action*/'x'/*%end*/
```

> [!IMPORTANT]
> Omitting an anchor produces a dangling `AND`, a trailing comma, or an empty clause, and
> therefore invalid SQL. This is an intentional consequence of the explicit model:
> predictability is preferred over implicit correction.

### Quoting and escaping

Quotation of string literals and quoted identifiers follows the SQL-standard doubling
convention in all three quote forms:

```sql
'it''s'      -- single-quoted string
"a""b"       -- double-quoted identifier
`a``b`       -- backtick-quoted identifier
```

> [!NOTE]
> Backslash escaping (for example, `'it\'s'`) is dialect-specific and is **not** recognized;
> such input is mis-lexed. The lexer is deliberately dialect-agnostic, and the doubling
> convention is portable across supported dialects.

### Reserved comment syntax

A block comment whose content begins with an identifier, whitespace, or a quote is
interpreted as a **bind directive**. To write an ordinary block comment, begin it with a
non-identifier character (for example, `/** … */`), or use a parser comment (`/*%! … */`),
which is removed from the output.

### Dynamic identifiers

bisql performs no raw-text substitution of arbitrary values. To sort or filter by a
runtime-selected column, enumerate the permitted columns as `/*%if*/` branches (a whitelist);
a column name originating from input is never interpolated. `@include` composes only static,
developer-registered fragments.

### Dialect portability

The following constructs vary by dialect. The author is responsible for selecting SQL that is
valid for the target database.

| Construct                       | Portability                                                              |
|:--------------------------------|:-------------------------------------------------------------------------|
| `1 = 1` / `1 = 0` anchors       | Universal.                                                               |
| `true` / `false` literals       | PostgreSQL, MySQL. Not supported by older SQL Server or Oracle; use `1 = 1` / `1 = 0`. |
| `IN (...)` placeholder expansion| Universal.                                                              |
| `= ANY($1::type[])` array bind  | PostgreSQL only (requires a native array type and driver support).      |
| `order by null` (no-op sort)    | MySQL, PostgreSQL, Oracle. SQL Server rejects a bare constant in `ORDER BY`; use `order by (select null)`. |

Placeholders themselves are generated per dialect and are selected with `WithDialect`:

| Dialect     | Placeholder form |
|:------------|:-----------------|
| MySQL       | `?`              |
| PostgreSQL  | `$1`, `$2`, …    |
| Oracle      | `:1`, `:2`, …    |
| SQL Server  | `@p1`, `@p2`, …  |

## Expression language

Directive expressions — the condition of `/*%if*/`, the iterable of `/*%for*/`, and the
expression of a bind — are evaluated by [expr-lang](https://github.com/expr-lang/expr). The
supported syntax includes comparison operators, the logical operators `&&`, `||`, and `!`,
property and method access, optional chaining (`a?.b`), the nil-coalescing operator (`??`),
the membership operator `in`, and `len`.

The null literal is `nil`; the Komapper idiom `x != null` is also accepted, because an
undefined identifier `null` resolves to nil. A nil or absent `/*%if*/` condition is treated
as false; a nil or absent `/*%for*/` iterable yields zero iterations. The evaluator is
replaceable through `WithEvaluator`.

## Package layout

```text
bisql            Public API: NewParser, Parser, Parse, ParseFile, Expand, ExpandFile,
                 Template, Statement, Option, and fragment loaders (Loader, RegistryLoader,
                 FSLoader, LoaderFunc, StackedLoader, ErrNotFound, WithLoader, WithStackedLoader).
dialect/         Dialect definitions: placeholder generation and literal formatting
                 (MySQL, PostgreSQL, Oracle, SQL Server).
expr/            Evaluator interface and Scope (for custom evaluators).
internal/
  sqltmpl/       Template layer: token, ast, lexer, parser, render, preprocess.
  exprlang/      Default evaluator (expr-lang).
docs/design.md   Design rationale for the explicit model.
```

## Development

The toolchain (Go, golangci-lint, deadcode) is pinned in `mise.toml`:

```bash
mise install
mise run check   # fmt, build, vet, lint, deadcode, and go test -race
```

Integration tests that require a live PostgreSQL instance are guarded by a build tag:

```bash
BISQL_TEST_PG_DSN='postgres://user:pw@localhost:5432/db?sslmode=disable' \
  go test -tags integration -run TestIntegrationPostgres ./...
```

## License

MIT
