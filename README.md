# bisql

**2-Way SQL template engine for Go** — syntax-compatible with
[Komapper](https://www.komapper.org/)'s TEMPLATE API, with a **first-class `include`**
(via the Komapper partial syntax `/*> name */`).

> ⚠️ **Status: scaffolding (M0).** Types, the public API shape, the design docs, and a full
> analysis of Komapper's implementation are in place. The lexer / parser / evaluator
> bodies come next (see [docs/roadmap.md](docs/roadmap.md)). The spec tests in
> `bisql_test.go` are written and skipped per milestone.

## What is 2-way SQL

Writing directives as **SQL comments** keeps a template runnable as-is in a SQL client,
while an application toggles conditions, iterates, and reuses fragments.

```sql
SELECT emp_no, name FROM employees
WHERE 1 = 1
/*%if name != null */
  AND name = /* name */'SCOTT'
/*%end */
/*%if employmentTypes != null */
  AND employment_type IN /* employmentTypes */('FULL_TIME')
/*%end */
ORDER BY emp_no
```

- `/*%if*/` is a block comment and `/* name */'SCOTT'` is a comment plus a literal, so you
  can **paste this into a DB client and run it** (it tries `SCOTT`).
- From an application, bisql toggles conditions based on `name`, binds values as
  placeholders, **drops conditions** that are not set, and drops a leading `AND` left
  dangling.

## Why (differences from Komapper)

bisql keeps Komapper's TEMPLATE syntax but makes **`include` a first-class runtime
feature**.

In Komapper, splicing a fragment via `/*# */` (Embedded SQL Variables) does **not
re-parse** the text: a fragment containing `/*%if*/` leaves the directive as a comment and
silently misbehaves. `/*> */` (Partial) is not supported by the runtime
`TemplateStatementBuilder` — it only works via KSP code generation (`@KomapperCommand`).

bisql's `/*> name */` **re-parses the fragment into nodes and splices it into the tree**,
so `/*%if*/` and binds inside the fragment work.

> The rationale follows the `@include` discussion in the article
> "静的 SQL ジェネレータはなぜ Oracle と相性が悪いのか".

## Usage (intended API)

```go
import (
    "github.com/mpyw/bisql"
    "github.com/mpyw/bisql/pkg/dialect"
)

tmpl, err := bisql.Parse(sqlText, bisql.WithDialect(dialect.MySQL))
stmt, err := tmpl.Build(map[string]any{"name": "SCOTT", "job": nil})

stmt.SQL          // "... WHERE name = ?"        for execution
stmt.Args         // []any{"SCOTT"}
stmt.SQLWithArgs  // "... WHERE name = 'SCOTT'"  for snapshots / review

// include via partial
ld := bisql.NewLoader(bisql.WithDialect(dialect.MySQL))
ld.Register("active", `/*%if activeOnly*/retired = /*zero*/0/*%end*/`)
tmpl, err = ld.Parse(`select emp_no from employees where /*> active */`)
```

## Directives

| syntax | meaning |
|---|---|
| `/* expr */literal` | bind placeholder (`literal` is the 2-way test value) |
| `/*^ expr */literal` | SQL literal embed |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` | conditional |
| `/*%for x in xs*/ … /*%end*/` | iteration |
| `/*> name */` | **partial = include, re-parsed and spliced (bisql runtime)** |
| `/*%! ... */` | parser-level comment (removed from output) |
| `/*# expr */` | raw-text embed (not re-parsed; **discouraged**) |

Full behavior and expected outputs:
[docs/komapper-template-features.md](docs/komapper-template-features.md).

## Layout

```
bisql            (root) Parse / Template / Statement / Option / Loader
internal/
  sqltmpl/{token,ast,lexer,parser,render}   SQL template layer
  exprlang/                                 built-in expression evaluator
pkg/
  dialect/       Dialect + placeholders (MySQL/PostgreSQL/Oracle/SQLServer)
  expr/          Evaluator interface + Scope (plug your own)
docs/
```

Public sub-packages live under `pkg/`; private ones under `internal/`. The heart of 2-way
is the `available`-flag evaluator in `internal/sqltmpl/render`.

## Documentation

- [docs/komapper-analysis.md](docs/komapper-analysis.md) — how Komapper works internally
- [docs/komapper-template-features.md](docs/komapper-template-features.md) — full syntax
  inventory (the TDD backlog with expected outputs)
- [docs/design.md](docs/design.md) — bisql design
- [docs/roadmap.md](docs/roadmap.md) — milestones

## Development

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

## License

MIT
