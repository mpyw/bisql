# bisql design notes

## Goal

A **2-way SQL template engine for Go**. Syntax-compatible with Komapper's TEMPLATE API,
with a **first-class include** exposed through the Komapper partial syntax (`/*> name */`).
Templates stay pasteable into a SQL client (2-way) while an application toggles
conditions, iterates, and reuses fragments.

Background and the Komapper internals are in
[`komapper-analysis.md`](./komapper-analysis.md); the full syntax inventory (the TDD
backlog) is in [`komapper-template-features.md`](./komapper-template-features.md).

## Non-goals

- Parsing SQL as a grammar. Like Komapper, bisql stays at a shallow structural
  tokenization.
- ORM / query builder / migrations. bisql only converts **template → (SQL, args)**.
- Connection handling / execution. The caller passes the result to `database/sql` etc.

## Package layout

Fine-grained on purpose (namespace hygiene). Public sub-packages under `pkg/`, private
ones under `internal/`, so the top level stays tidy.

```
bisql            (root) Parse / Template / Statement / Option / Loader
internal/
  sqltmpl/       SQL template layer (shallow structure)
    token/       Kind + kind consts
    ast/         Node model + Location
    lexer/       Lexer (branches right after "/*"; recognizes clause keywords)
    parser/      Parse -> ast.Node (reducer-stack strategy)
    render/      Render (the available-flag evaluator; heart of 2-way)
  exprlang/      built-in expression evaluator (resolves via reflection)
pkg/
  dialect/       Dialect, Placeholder, MySQL/PostgreSQL/Oracle/SQLServer
  expr/          Evaluator interface, Scope (public: callers can plug their own)
```

The root package exposes only `Parse / Template.Build / Statement / Loader / Option`.
Internal token/ast types never leak into the public API.

## Public API (intended)

```go
// Parse once; Build many times. Template is immutable and safe for concurrent use.
tmpl, err := bisql.Parse(sqlText, bisql.WithDialect(dialect.MySQL))
stmt, err := tmpl.Build(map[string]any{"name": "SCOTT", "job": nil})

stmt.SQL          // "... WHERE name = ?"        for execution
stmt.Args         // []any{"SCOTT"}
stmt.SQLWithArgs  // "... WHERE name = 'SCOTT'"  for snapshots / review

// include via partial: register fragments, reference with /*> name */
ld := bisql.NewLoader(bisql.WithDialect(dialect.MySQL))
ld.Register("active", `/*%if activeOnly*/retired = /*zero*/0/*%end*/`)
// or: ld.LoadFS(os.DirFS("sql"), "**/*.sql")
tmpl, err = ld.Parse(`select emp_no from employees where /*> active */`)
```

`Build` accepts `map[string]any`, `expr.Scope`, or a struct (fields via reflection).

## Directive spec

Komapper-compatible. See [`komapper-template-features.md`](./komapper-template-features.md)
for exact behaviors and expected outputs.

| syntax | meaning |
|---|---|
| `/* expr */literal` | bind placeholder (`literal` is the 2-way test value) |
| `/*^ expr */literal` | SQL literal embed (dialect formats; injection-prone) |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` | conditional |
| `/*%for x in xs*/ … /*%end*/` | iteration (`x_index/x_has_next/x_next_comma/and/or`) |
| `/*> name */` | **partial = bisql's include (re-parsed and spliced)** |
| `/*%! ... */` | parser-level comment (removed from output) |
| `/*# expr */` | raw-text embed (**discouraged**; not re-parsed) |

### Include design (the core)

- `/*> name */` keeps 2-way (a block comment; pasteable).
- On resolution, the fragment text is run through **lexer → parser to get a node subtree,
  then spliced in place**, so `/*%if*/` and binds inside the fragment work — unlike
  Komapper's `/*# */` raw embed.
- Cyclic references are detected and error out; a missing fragment errors out.
- Two modes are envisioned:
  - **runtime**: `Loader.Parse` follows partials into a single tree.
  - **ahead-of-time (optional)**: a tool expands partials into plain 2-way SQL files that
    can be run through `EXPLAIN` in CI.

## The heart of 2-way (faithful port)

`internal/sqltmpl/render` ports Komapper's `available`-flag approach:

- only real tokens (Word/Other) set `available = true`;
- a clause emits its keyword only when its body is available (drops empty clauses);
  Select/From are always kept;
- AND/OR emits its keyword only when preceding content is available (drops dangling
  connectors);
- blanks (Space/Eol) are buffered and flushed so removed content leaves no stray space.

## Expression evaluator

- `pkg/expr.Evaluator`: `Eval(expression string, scope Scope) (any, error)`.
- Default (`internal/exprlang`): a small language (comparisons, logical ops, literals,
  property/method access, safe call) resolving map keys / struct fields / methods via
  reflection.
- Swappable via `bisql.WithEvaluator`.

## Dialect

- Abstracts only placeholder generation (and, later, literal formatting):
  MySQL `?`, PostgreSQL `$n`, Oracle `:n`, SQL Server `@pn`.

## Milestones

See [`roadmap.md`](./roadmap.md).
