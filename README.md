# bisql

**2-Way SQL template engine for Go** — syntax-compatible with
[Komapper](https://www.komapper.org/)'s TEMPLATE API, with a **first-class `include`**
(via the Komapper partial syntax `/*> name */`).

> **Status: functional.** Lexer, parser, expression evaluator (backed by
> [expr-lang](https://github.com/expr-lang/expr)), the 2-way renderer, and recursive
> include/embedded expansion are implemented and tested; see
> [docs/roadmap.md](docs/roadmap.md). The one deferred item is the optional ahead-of-time
> `cmd/bisql-expand` tool. API may still change before a tagged release.

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
feature** — and extends it to embedded values too.

In Komapper, splicing a fragment via `/*# */` (Embedded SQL Variables) does **not
re-parse** the text: a fragment containing `/*%if*/` leaves the directive as a comment and
silently misbehaves. `/*> */` (Partial) is not supported by the runtime
`TemplateStatementBuilder` — it only works via KSP code generation (`@KomapperCommand`).

bisql **re-parses and splices recursively** for both directives, so `/*%if*/`, binds, and
further embeds/partials inside the spliced text all work. The two differ only in **source**:

- `/*> name */` (**partial**) — a static fragment registered on a `Loader`. Safe: only
  developer-registered text is parsed.
- `/*# expr */` (**embedded value**) — a string produced by evaluating a runtime scope
  expression. Powerful for injecting a dynamically-built SQL snippet, but because the text
  comes from data it is an **injection surface**: only trusted, developer-controlled values
  should flow through it.

Recursion is bounded by a depth limit, and cyclic partial references are reported as errors.

> The rationale follows the `@include` discussion in the article
> "静的 SQL ジェネレータはなぜ Oracle と相性が悪いのか".

## Usage

```go
import (
    "github.com/mpyw/bisql"
    "github.com/mpyw/bisql/dialect"
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
| `/*> name */` | **partial = include a named static fragment, re-parsed and spliced recursively** |
| `/*%! ... */` | parser-level comment (removed from output) |
| `/*# expr */` | **embedded value = splice a runtime string, re-parsed recursively** (trusted text only) |

Full behavior and expected outputs:
[docs/komapper-template-features.md](docs/komapper-template-features.md).

## Authoring notes / limitations

Because bisql does not parse SQL as a grammar (shallow structural tokenization, like
Komapper), a few things are the template author's responsibility:

- **Empty grouping parens are not removed.** `where (/*%if a*/a = 1/*%end*/)` with `a`
  false renders `where ()` — a dropped group is indistinguishable from a call like
  `count()`. Guard the whole group with an outer `/*%if*/` instead.
- **Dynamic WHERE without the `1 = 1` idiom** works (a leading `AND`/`OR` left dangling is
  dropped, and an empty `WHERE` is removed) as long as `where` is a real clause keyword,
  not nested inside the first `/*%if*/`.
- **Identifiers that spell a clause / set-operator keyword** (`select`, `where`, `union`,
  …) must be quoted; unquoted, they are tokenized as the keyword.

## Layout

```
bisql            (root) Parse / Template / Statement / Option / Loader
dialect/         Dialect + placeholders (MySQL/PostgreSQL/Oracle/SQLServer)
expr/            Evaluator interface + Scope (plug your own)
internal/
  sqltmpl/{token,ast,lexer,parser,render}   SQL template layer
  exprlang/                                 built-in expression evaluator
docs/
```

Public sub-packages sit at the top level (`dialect`, `expr`); private ones under
`internal/`. The heart of 2-way is the `available`-flag evaluator in
`internal/sqltmpl/render`.

## Documentation

- [docs/komapper-analysis.md](docs/komapper-analysis.md) — how Komapper works internally
- [docs/komapper-template-features.md](docs/komapper-template-features.md) — full syntax
  inventory (the TDD backlog with expected outputs)
- [docs/design.md](docs/design.md) — bisql design
- [docs/roadmap.md](docs/roadmap.md) — milestones

## Development

Toolchain (Go, golangci-lint, deadcode) is pinned in `mise.toml` — install [mise](https://mise.jdx.dev),
then:

```sh
mise install       # fetch the pinned tools
mise run check     # fmt + build + vet + lint + deadcode + test -race
mise run test      # just the tests
```

## License

MIT
