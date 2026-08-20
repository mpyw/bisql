<p align="center">
  <img src="https://github.com/user-attachments/assets/0088bc40-c207-43ee-adfe-77d9562430de" alt="bisql" width="180">
</p>

# bisql

[![CI](https://github.com/mpyw/bisql/actions/workflows/ci.yml/badge.svg)](https://github.com/mpyw/bisql/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/mpyw/bisql/graph/badge.svg)](https://codecov.io/gh/mpyw/bisql)
[![Go Reference](https://pkg.go.dev/badge/github.com/mpyw/bisql.svg)](https://pkg.go.dev/github.com/mpyw/bisql)

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
└── users/
    ├── search.sql     root template
    └── _active.sql    reusable fragment
```

```sql
-- sql/users/search.sql
select id, name
from users
where 1 = 1
/*%if name != null*/and name = /*name*/'Alice'/*%end*/
/*%! @include users/_active.sql */
order by id
```

```sql
-- sql/users/_active.sql
/*%if activeOnly*/and status = /*status*/'active'/*%end*/
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

	tmpl, err := p.ParseFile(sqlFS, "users/search.sql")
	if err != nil {
		panic(err)
	}

	stmt, err := tmpl.Build(map[string]any{
		"name":       "Alice",
		"activeOnly": true,
		"status":     "active",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(stmt.SQL)  // parameterized form; execute this
	fmt.Println(stmt.Args) // []any{"Alice", "active"}
}
```

The resulting statement is:

```sql
select id, name
from users
where 1 = 1
and name = $1
and status = $2
order by id
```

A `Parser` is immutable and safe for concurrent use; it is constructed once and reused across
templates. A string template, rather than a file, is parsed with `p.Parse` instead of
`p.ParseFile`. The package-level `bisql.Parse`, `bisql.ParseFile`, `bisql.Expand`, and
`bisql.ExpandFile` functions are shortcuts that construct a single-use parser from the given
options.

`Build` accepts the parameters as a `map[string]any`, a **struct**, or an `expr.Scope`. Passing a
struct matches exported fields to bind names (an embedded struct's fields are promoted, and also
reachable qualified, e.g. `Filter.Status`), so parameters carry Go's type checking rather than
being untyped map values.

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
select id, name from users
where 1 = 1
/*%if name != null*/and name = /*name*/'Alice'/*%end*/
order by id
```

(A template may instead spell its binds the way sqlc does, giving up this property for
values in exchange for having them typed by an analyzer; see [Bind syntax](#bind-syntax).)

When the text is executed verbatim in a SQL client, `/*%if*/`, `/*%end*/`, and `/*name*/` are
interpreted as comments, and the trailing literal `'Alice'` remains in place; the client
therefore evaluates `name = 'Alice'`. When the same text is processed by bisql, the `/*%if*/`
condition is evaluated, the fragment `/*name*/'Alice'` is replaced by a placeholder, and the
value is bound as an argument. The two interpretations are designed to remain semantically
consistent.

## The explicit model

bisql **performs no implicit removal.** The renderer emits the template verbatim; it only
evaluates directives (bind, literal, conditional, iteration) and strips parser comments. In
particular, the engine does not remove empty clauses, does not delete dangling `AND`/`OR`
connectors, and does not normalize whitespace.

This design makes the output a deterministic function of the template text and the evaluated
branches, at the cost of a single authoring obligation: **the author anchors every dynamic
fragment** so that no connector is ever left dangling. The `1 = 1` above serves this purpose.
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
where name = /*name*/'Alice'

-- name = "Alice"
--   →  where name = $1
--      ($1 = "Alice")
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

</td>
<td>

**Iteration.** Renders the body once for each element of `xs`, bound to `x`, with **no text
inserted between iterations**. A list is kept two-way by anchoring it and having each iteration
lead with its own connector (see [Building lists](#building-lists)).

```sql
where 1 = 0 /*%for kw in kws*/or name like /*kw*/'%x%'/*%end*/

-- kws = ["a", "b"]
--   →  where 1 = 0 or name like $1 or name like $2
--      ($1 = "a", $2 = "b")
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
where 1 = 1 /*%! @include users/_active.sql */
-- → where 1 = 1 <text of users/_active.sql>
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
select id /*%! TODO: drop this column */ from users
-- → select id  from users
```

</td>
</tr>
</tbody>
</table>

All other comment forms — block comments (`/** … */`), line comments (`-- …`), and optimizer
hints (`/*+ … */`) — pass through to the output unchanged.

### Building lists

`/*%for*/` inserts nothing between iterations, so a list is made two-way the same way every
dynamic fragment is: a fixed **anchor** plus a **leading connector** on each element.

- **`AND` / `OR` lists** anchor with `1 = 1` / `1 = 0`, and each iteration leads with its own
  `and` / `or`. An empty list renders only the anchor.
- **Comma lists** — a multi-row `VALUES`, a `jsonb_build_object`, an `ARRAY[…]` — have no no-op
  slot to anchor against, so they are expressed as a **set**: a zero-row `select … where 1 = 0`
  seed followed by one `union all select …` per element, consumed by a table source or an
  aggregate (`jsonb_agg`, `array_agg`, `jsonb_object_agg`, …). `union all` is the leading
  connector, and an empty list renders only the seed. This is empty-safe, unlike a bare comma
  list, which leaves a dangling comma or an invalid empty `values`.

```sql
-- multi-row insert, empty-safe (an empty list inserts nothing)
insert into audit_logs (user_id, action)
select 0, '' where 1 = 0
/*%for log in logs*/ union all select /*log.userId*/0, /*log.action*/'view'/*%end*/
```

| Context               | Result                                                          |
|:----------------------|:---------------------------------------------------------------|
| Build (two rows)      | `… where 1 = 0 union all select $1, $2 union all select $3, $4` |
| Raw paste in a client | `… where 1 = 0 union all select 0, 'view'` — a valid statement  |
| Empty list            | `… where 1 = 0` — inserts nothing                              |

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
tmpl.Build(map[string]any{"ids": []int{1, 2, 3}})
// SQL:  where id in ($1, $2, $3)
// Args: []any{1, 2, 3}
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
tmpl.Build(map[string]any{"ids": []int{1, 2, 3}})
// SQL:  where id = ANY($1::int[])
// Args: []any{[]int{1, 2, 3}}
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

> [!TIP]
> Fragments compose in either of two styles, differing in which side owns the connecting `and`:
>
> ```sql
> -- (a) the fragment owns the connector — the style used throughout this document
> where 1 = 1 /*%! @include active */
> --   fragment "active": /*%if activeOnly*/and status = /*status*/'active'/*%end*/
>
> -- (b) the fragment is self-contained; the including template owns the connector
> where 1 = 1 and /*%! @include active */
> --   fragment "active": 1 = 1 /*%if activeOnly*/and status = /*status*/'active'/*%end*/
> ```
>
> Both build to valid SQL (b carries a harmless extra `1 = 1`). They differ only in the
> un-expanded template: in (a) the `@include` is a comment, leaving `where 1 = 1`, so the
> template itself runs in a client; in (b) the `and` dangles until `Expand` resolves the
> include. Choose (a) to keep the un-expanded template runnable, or (b) for self-contained
> fragments, treating `Expand` as the boundary that yields the two-way statement.

Fragment resolution is delegated to an implementation of the `Loader` interface:

```go
type Loader interface {
	Load(name string) (string, error)
}
```

Four implementations are provided. There is **no default**; a template that uses `@include`
must be parsed with a loader.

| Implementation   | Constructor                        | Source                                              |
|:-----------------|:-----------------------------------|:----------------------------------------------------|
| `RegistryLoader` | `bisql.NewRegistryLoader()`              | In-memory fragments registered by name.             |
| `FSLoader`       | `bisql.NewFSLoader(fs.FS)`         | Files in an `fs.FS` (`embed.FS`, `os.DirFS`, …); the `@include` name is the file path from the root of the `fs.FS`. |
| `LoaderFunc`     | `bisql.LoaderFunc(fn)`             | An adapter over any resolution function.            |
| `StackedLoader`  | `bisql.NewStackedLoader(…Loader)`  | A chain of loaders tried in order (see below).      |

`ParseFile` (see [Synopsis](#synopsis)) is the file-oriented entry point: it reads the root
template from an `fs.FS` and, unless a loader is configured explicitly, resolves `@include`
fragments from the same `fs.FS`. It is equivalent to the following, which applies when the
root template is a string rather than a file:

```go
loader := bisql.NewFSLoader(sqlFS)
tmpl, err := bisql.Parse(rootSrc, bisql.WithLoader(loader))
```

Fragments may also be provided in memory rather than as files:

```go
loader := bisql.NewRegistryLoader().
	Register("active_filter", "/*%if activeOnly*/and status = /*status*/'active'/*%end*/")

tmpl, err := bisql.Parse(
	"select id from users where 1 = 1 /*%! @include active_filter */",
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
expanded, err := bisql.ExpandFile(sqlFS, "users/search.sql")
```

> [!TIP]
> Pin **two snapshots per query** as a regression guard:
>
> - **Expansion** — the `Expand`/`ExpandFile` output (`@include` resolved, still two-way).
>   Dialect- and parameter-independent; isolates *what was composed*.
> - **Build** — the `SQL` + `Args` (or the values-embedded `SQLWithArgs`) for representative
>   parameters, per dialect; captures *what an input produces*.
>
> The sample project under [`testdata/example`](testdata/example) does this: one
> `<name>.expanded.sql` per query, one `<name>.<case>.embedded.sql` per case.

## Authoring rules

Because the engine removes nothing implicitly, a template author observes the following rules.

### Anchoring dynamic fragments

Each dynamic construct is preceded by a fixed anchor so that a rendered connector is never
left dangling.

| Construct         | Anchoring rule                                                                          |
|:------------------|:----------------------------------------------------------------------------------------|
| `WHERE` / `HAVING`| Introduce a constant predicate (`1 = 1` for `AND` chains, `1 = 0` for `OR` chains); each condition carries a leading `and`/`or`. |
| `ORDER BY`        | Terminate the list with a stable key (for example, `id`).                               |
| `SELECT` / `SET` column list | Begin with a fixed column and add each optional, whitelisted column with a leading comma inside a `/*%if*/`. |
| List via `/*%for*/` | Anchor the list and have each iteration lead with its own connector — `and`/`or` for a predicate list, or `union all select …` off a zero-row `select … where 1 = 0` seed for a comma/row list (see [Building lists](#building-lists)). |
| `JOIN` / `UNION`  | Place the connector inside the `/*%if*/` block so that each fragment is self-contained. |

```sql
-- WHERE: constant predicate + leading connectors
select * from users
where 1 = 1
/*%if name != null*/and name = /*name*/'Alice'/*%end*/
/*%if minAge != null*/and age >= /*minAge*/20/*%end*/

-- ORDER BY: trailing stable key
select * from users
order by /*%if byName*/name, /*%end*/id

-- SELECT list: fixed leading column; optional known columns added with a leading comma.
-- Columns are whitelisted with /*%if*/, never bound.
select id /*%if withName*/, name/*%end*/ /*%if withDept*/, department_id/*%end*/
from users

-- Row list: no anchor slot, so express it as a set — a zero-row seed plus one
-- "union all select" per row. Empty-safe: an empty list inserts nothing.
insert into audit_logs (user_id, action)
select 0, '' where 1 = 0
/*%for log in logs*/ union all select /*log.userId*/0, /*log.action*/'view'/*%end*/
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
| SQLite      | `?`              |
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
as false; a nil or absent `/*%for*/` iterable yields zero iterations. Everything after `in` in
a `/*%for*/` directive is the iterable expression verbatim — including any colons (a ternary
`a ? b : c`, a slice `x[1:2]`, a map `{k: v}`). The evaluator is replaceable through
`WithEvaluator`.

## Bind syntax

A bind is written as `/*expr*/literal` by default, and that shape is what makes a template
runnable as-is: the comment is ignored and the sample literal takes its place. The cost
appears when the template is also meant to be read by a static analyzer such as
[sqlc](https://sqlc.dev). Being runnable requires a **literal** at the bind site; being
recognized as a parameter requires a **marker**. No single text is both, so an analyzer
reading a two-way template sees constants where the binds are — it can check the SQL, the
catalog, and the result columns, but it can say nothing about the arguments.

`WithBindSyntax(bindsyntax.SqlcNamed)` trades the runnable-as-is property, for values only,
to get that back:

```go
tmpl, err := bisql.Parse(src,
    bisql.WithBindSyntax(bindsyntax.SqlcNamed),
    bisql.WithDialect(dialect.PostgreSQL))
```

| Form | Binds | Notes |
| ---- | ----- | ----- |
| `@name` | one parameter | the name is a bare identifier; `@a.b` is an error, not a dotted name |
| `sqlc.arg('name')` | one parameter | the name may contain dots (`'c.name'`), for a value reached through a field |
| `sqlc.narg('name')` | one parameter | identical at build time; the distinction is for the analyzer |
| `sqlc.slice('name')` | a placeholder list | the parentheses stay in the template, as `in (sqlc.slice('ids'))` |

```sql
select id from users
where 1 = 1
  /*%if activeOnly*/ and status = @status /*%end*/
  /*%if minAge != null*/ and age >= @min_age /*%end*/
  /*%for kw in keywords*/ and name like @kw /*%end*/
  and id in (sqlc.slice('ids'))
```

**Only the bind spelling changes.** Every block directive is a SQL comment under either
syntax, so `/*%if*/`, `/*%elseif*/`, `/*%else*/`, `/*%for*/` and `@include` behave
identically, and placeholder numbering is the same single renderer-global counter — binds in
branches that did not render are still never counted.

The two forms that depend on a test literal have no meaning without one, so `Parse` rejects
them rather than reinterpreting them:

- **The two-way bind directive.** `/*status*/'active'` would otherwise be read as a plain
  comment followed by a literal, producing a query that runs while ignoring a value.
- **`/*^ */` literal interpolation.** The value is inlined as text rather than bound, so an
  analyzer sees a constant there and can check nothing about it. Bind the value as a
  parameter, or use a whitelisted `/*%if*/` toggle for an identifier or a sort direction.

A prefix that could only have been meant as a marker but cannot be one is rejected for the
same reason, since nothing downstream parses the SQL to catch it:

```sql
where name = @c.name         -- error: a dotted name must be sqlc.arg('c.name')
where name = sqlc.arg(x)     -- error: the name has to be single-quoted
```

`@c.name` would otherwise bind only `c` and render as `$1.name`, and `sqlc.arg(x)` would be
emitted verbatim as a call to a function that does not exist. sqlc makes the same reading of
`@c.name` and then rejects the edited query; bisql has to reject it up front instead.

The two syntaxes are not mirror images of each other. A named marker is opaque under the
default syntax — `@status` is a plain word there — but a two-way directive under `SqlcNamed`
is an error rather than text, because reading it as a comment followed by a literal would
give a query that runs while ignoring a value.

Recognizing `@name` requires that what follows the `@` can start an identifier, so `@>` and
`@@version` are left alone under either syntax. A **MySQL user variable is not**: under
`SqlcNamed` a single `@` followed by a name reads as a bind.

```sql
-- sqlc-named, MySQL
select @row_number := @row_number + 1   -- renders as: select ? := ? + 1
```

sqlc reads it the same way, so a template meant for sqlc could not use one regardless. A
query that needs user variables belongs on the default syntax.

## Package layout

```text
bisql            Public API: NewParser, Parser, Parse, ParseFile, Expand, ExpandFile,
                 Template, Statement, Option, and fragment loaders (Loader, RegistryLoader,
                 FSLoader, LoaderFunc, StackedLoader, ErrNotFound, WithLoader, WithStackedLoader).
bindsyntax/      How a bind is written: TwoWay (the default /*expr*/literal form) and
                 SqlcNamed (@name / sqlc.arg('name'), not implemented yet).
dialect/         Dialect definitions: placeholder generation and literal formatting
                 (MySQL, SQLite, PostgreSQL, Oracle, SQL Server).
expr/            Evaluator interface and Scope (for custom evaluators).
internal/
  sqltmpl/       Template layer: token, ast, lexer, parser, render, preprocess.
  exprlang/      Default evaluator (expr-lang).
```

The library depends only on `expr-lang` (`pgx` is a test-only dependency).

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
