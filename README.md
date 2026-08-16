# bisql

**2-Way SQL template engine for Go.** Directives are written as SQL comments, so a template
stays runnable as-is in a SQL client while an application toggles conditions, iterates, and
composes fragments. The syntax is inspired by [Komapper](https://www.komapper.org/)'s
TEMPLATE API; the semantics are bisql's own **explicit model** (see below).

```go
tmpl, err := bisql.Parse(sqlText, bisql.WithDialect(dialect.PostgreSQL))
stmt, err := tmpl.Build(map[string]any{"name": "SCOTT", "minAge": 20})

stmt.SQL             // "... where 1 = 1 and name = $1 and age >= $2"  (execute this)
stmt.Args            // []any{"SCOTT", 20}
stmt.SQLWithArgs()   // "... where 1 = 1 and name = 'SCOTT' and age >= 20"  (review only; never execute)
```

`bisql.Parse` is a shortcut. To configure the dialect/evaluator/loader once and parse many
templates, build a `Parser` and reuse it:

```go
p := bisql.NewParser(bisql.WithDialect(dialect.PostgreSQL))
t1, _ := p.Parse(sqlA)
t2, _ := p.Parse(sqlB) // same config, no repeated options
```

`SQLWithArgs()` is a method computed on demand: a statement you only execute never pays to
build the values-embedded review string.

## What is 2-way SQL

Writing directives as **SQL comments** keeps a template runnable as-is in a client:

```sql
select emp_no, name from employees
where 1 = 1
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
order by emp_no
```

Pasted raw, this runs (the `/*%if*/` are comments and `/*name*/'SCOTT'` is a comment plus a
literal, so the client tries `name = 'SCOTT'`). From an application, bisql evaluates the
`/*%if*/`, replaces `/*name*/'SCOTT'` with a placeholder, and binds the value.

## The explicit model (important)

bisql **removes nothing implicitly.** The renderer emits your template **verbatim**, only
evaluating directives (bind/literal/if/for/with) and dropping parser comments. There is no
empty-clause removal, no dangling `AND`/`OR` cleanup, no whitespace magic.

This makes output completely predictable, at the cost of a small discipline: **you anchor
your dynamic SQL** so nothing is ever left dangling. That is what the `1 = 1` above is for.
See [Authoring rules](#authoring-rules--gotchas).

## Directives

| syntax | meaning |
|---|---|
| `/* expr */literal` | **bind**: placeholder + bound value (`literal` is the 2-way test value) |
| `/*^ expr */literal` | **literal**: inline the value as a SQL literal (review/DDL; injection-prone) |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` | conditional |
| `/*%for x in xs*/ … /*%end*/` | iteration (exposes `x_index`, `x_has_next`) |
| `/*%! ... */` | parser comment: removed from output (also hosts `@include`) |
| `/*%! @include name */` | **preprocessor**: splice a registered fragment (see [Includes](#includes)) |

Plain comments (`/** ... */`, `-- ...`, optimizer hints `/*+ ... */`) pass through verbatim.

### IN lists vs. array binds

The **shape of the test literal** decides how a bind expands:

- **`(...)` test → placeholder list.** `id in /*ids*/(1, 2)` with `ids=[]int{1,2,3}` →
  `id in (?, ?, ?)`. An empty slice → `(null)`; a slice-of-slices → row tuples
  `((?, ?), (?, ?))`.
- **scalar test → one parameter as-is.** `= ANY(/*ids*/'{}'::int[])` with `ids=[]int{...}`
  binds the whole slice as **one array parameter** → `= ANY($1::int[])`. (Postgres only;
  see the portability table.)

## Includes

`@include` is a **preprocessing** step: `/*%! @include name */` is replaced by the named
fragment's text (recursively, with cycle detection) before parsing. Because it rides on the
`/*%! ... */` parser-comment channel, a raw template still runs in a client (the include is
just a comment; the base SQL runs without the fragment).

How a fragment is loaded is pluggable via the `Loader` interface (`Load(name) (string, error)`).
bisql ships two implementations — `RegistryLoader` (in-memory) and `FSLoader` (an `fs.FS`
such as `embed.FS` / `os.DirFS`) — plus a `LoaderFunc` adapter for anything else (a DB
table, a cache, …). There is **no default**: pass one with `WithLoader`.

```go
// in-memory
ld := bisql.NewRegistry().Register("active", "/*%if activeOnly*/retired = /*zero*/0/*%end*/")
tmpl, _ := bisql.Parse("select emp_no from employees where /*%! @include active */ 1 = 1",
    bisql.WithLoader(ld))

// from an fs.FS (the @include name is the file's path, extension included):
//   bisql.Parse(src, bisql.WithLoader(bisql.NewFSLoader(os.DirFS("sql"))))  // @include active.sql
// from anywhere:
//   bisql.Parse(src, bisql.WithLoader(bisql.LoaderFunc(func(name string) (string, error) { ... })))

expanded, _ := bisql.Expand(src, bisql.WithLoader(ld)) // resolved, still-2-way SQL (snapshots / EXPLAIN)
```

A bare `bisql.Parse` (no `WithLoader`) rejects `@include` — there is nothing to resolve.

## Authoring rules / gotchas

Because the engine removes nothing, follow these. Each is a one-liner with an example.

### 1. Anchor everything (the engine cleans nothing)

| dynamic part | idiom | example |
|---|---|---|
| WHERE / HAVING | `1 = 1` (AND-chain) or `1 = 0` (OR-chain) anchor; conditions carry a leading `and`/`or` | `where 1 = 1 /*%if a != null*/and a = /*a*/0/*%end*/` |
| ORDER BY | end with a stable key (`id`) | `order by /*%if byName*/name,/*%end*/ id` |
| SELECT / SET list | base column anchor + leading comma, or a `/*%for*/` loop | `select id /*%if a*/, a/*%end*/ from t` |
| list via loop | anchor + `/*%if x_has_next*/,/*%end*/` (there is **no** `_next_comma`) | `/*%for c in cols*//*c*/0/*%if c_has_next*/, /*%end*//*%end*/` |
| JOIN / UNION | put the connector **inside** the `/*%if*/` (self-contained) | `/*%if withB*/join b on …/*%end*/` |

Forget an anchor and you get dangling `AND`/`,` or a bare clause — invalid SQL. That is by
design; predictability over magic.

### 2. Escape quotes by doubling (not backslash)

Use SQL-standard **doubling** in all three quote kinds: `'it''s'`, `"a""b"`, `` `a``b` ``.
Backslash escapes (`'it\'s'`, MySQL-only, dialect-dependent) are **not** recognized and will
mis-lex — bisql's lexer is deliberately dialect-agnostic, and `''` works on MySQL too.

### 3. `/* ... */` is a bind directive

A block comment starting with an identifier/space/quote is read as a **bind**. Write a plain
block comment as `/** ... */` (leading `*`), or use a `/*%! ... */` parser comment. (`/*#`,
formerly "embedded", is now just a plain comment.)

### 4. Dynamic identifiers must be whitelisted

There is no raw-text substitution. To sort/filter by a runtime-chosen column, **toggle known
columns** with `/*%if*/` (a whitelist) — never interpolate a column name from input.
`@include` composes only static, developer-registered fragments.

### 5. Dialect portability

| construct | portability |
|---|---|
| `1 = 1` / `1 = 0` anchors | universal |
| `true` / `false` literals | Postgres/MySQL; **not** old SQL Server / Oracle (use `1=1`/`1=0`) |
| `IN (?, ?, …)` expansion | universal |
| `= ANY($1::type[])` array bind | **Postgres only** (needs a native array type + driver) |
| `order by null` (no-op sort) | MySQL/Postgres; SQL Server needs `order by (select null)` |

Placeholders themselves are handled per dialect: MySQL `?`, Postgres `$n`, Oracle `:n`,
SQL Server `@pn` — set via `bisql.WithDialect`.

## Expression language

Directive expressions (`/*%if e*/`, the bind expression, …) use
[expr-lang](https://github.com/expr-lang/expr): comparisons, `&&`/`||`/`!`, property/method
access, `a?.b`, `??`, `in`, `len`. The null literal is `nil`, but the Komapper idiom
`x != null` also works (an undefined `null` resolves to nil). A nil/absent `/*%if*/`
condition is falsy; a nil/absent `/*%for*/` iterable is zero iterations. Swap the evaluator
with `bisql.WithEvaluator`.

## Layout

```
bisql            (root) NewParser / Parser / Parse / Expand / Template / Statement / Option
                 include: Loader + RegistryLoader / FSLoader / LoaderFunc / WithLoader
dialect/         Dialect + placeholders (MySQL/PostgreSQL/Oracle/SQLServer) + literal format
expr/            Evaluator interface + Scope (plug your own)
internal/
  sqltmpl/{token,ast,lexer,parser,render,preprocess}   template layer
  exprlang/                                            default expr-lang evaluator
docs/design.md   design rationale (the explicit model)
```

## Development

Toolchain (Go, golangci-lint, deadcode) is pinned in `mise.toml`:

```sh
mise install
mise run check    # fmt + build + vet + lint + deadcode + go test -race
```

Real-Postgres integration tests are build-tagged:

```sh
BISQL_TEST_PG_DSN='postgres://user:pw@localhost:5432/db?sslmode=disable' \
  go test -tags integration -run TestIntegrationPostgres ./...
```

## License

MIT
