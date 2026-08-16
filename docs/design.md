# bisql design notes

## Goal

A **2-way SQL template engine for Go**: directives written as SQL comments, so a template is
runnable as-is in a SQL client, while an application converts it to `(SQL, args)`. The syntax
is inspired by Komapper's TEMPLATE API; the semantics are bisql's **explicit model**. For the
directive reference and authoring rules, see the [README](../README.md).

## Non-goals

- Parsing SQL as a grammar. bisql stays at directive-level tokenization; the SQL body is
  opaque.
- Implicit cleanup. bisql removes **nothing** it wasn't told to (see below).
- ORM / query builder / migrations / connection handling. bisql only produces
  `template → (SQL, args)`.

## The explicit model (the core decision)

Earlier iterations mirrored Komapper's `available`-flag machinery: empty clauses were
dropped, dangling `AND`/`OR` were removed, whitespace was normalized. That machinery is the
single largest source of complexity and edge-case bugs (placeholder-numbering across child
states, dangling connectors inside parens, whitespace artifacts). bisql abandons it.

**The renderer emits the template verbatim.** It only:

- evaluates `/* */` bind and `/*^ */` literal directives,
- chooses `/*%if*/` branches, iterates `/*%for*/`,
- drops `/*%! ... */` parser comments.

Everything else — clause keywords, connectors, commas, whitespace — passes through
unchanged. Output is therefore a pure function of the template text and the chosen branches,
trivially predictable.

The cost is an **authoring discipline**: the template must anchor its dynamic parts so no
separator is ever left dangling (`1 = 1` / `1 = 0`, a trailing `id`, connectors inside
`/*%if*/`, `/*%if x_has_next*/,/*%end*/` in loops). This is documented prominently in the
README's "Authoring rules". It is the well-understood MyBatis `1=1` style, made uniform.

Consequences that fall out of "emit verbatim":

- **No clause tokenization.** The lexer no longer recognizes `select`/`where`/`and`/`union`
  etc.; they are ordinary words. The whole clause/connector/set apparatus is gone.
- **Placeholder numbering** is one renderer-global counter. Binds in unrendered branches or
  unreached loop iterations are never visited, so numbering is gap-free for `$n`/`:n`/`@pn`.
- **Whitespace** is whatever the template says. `1 = 1\n\n\norder by id` keeps its blank
  lines — valid SQL, if not always pretty.

## Directives

| syntax | meaning |
|---|---|
| `/* expr */literal` | bind placeholder (`literal` is the 2-way test value) |
| `/*^ expr */literal` | inline SQL literal (dialect-formatted; injection-prone) |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` | conditional |
| `/*%for x in xs*/ … /*%end*/` | iteration; exposes `x_index`, `x_has_next` (usable in `/*%if*/`) |
| `/*%! ... */` | parser comment (removed); also hosts `@include` |
| `/*%! @include name */` | preprocessor: splice a static fragment |

Removed vs. earlier iterations / Komapper: `/*> name */` (partial), `/*# expr */`
(embedded), and `/*%with e*/` are **gone**; `/*#…*/` is now an ordinary comment.
Composition is `@include` only; there is no raw-text substitution. `/*%with*/` was dropped
as redundant sugar — expr-lang already does qualified access, so `/*criteria.min*/0` covers
what `/*%with criteria*/ /*min*/0 /*%end*/` did.

### Bind expansion is keyed on the test-literal shape

- `(...)` test → expand an iterable into a placeholder list (empty → `(null)`; slice
  elements → row tuples).
- scalar test → bind the value as-is, so a slice becomes a **single array parameter** for
  Postgres `= ANY($1::type[])`. Whether the driver/dialect supports array binding is the
  driver's concern (Postgres only in practice).

## @include preprocessing

`internal/sqltmpl/preprocess` runs before lexing. It scans the raw text (skipping string and
quoted-identifier spans and `--` line comments) for `/*%! @include name */`, and replaces
each with the resolved fragment text, recursively, with cycle detection and a depth bound.
The result is a fully expanded, still-2-way template. Fragment loading is pluggable via the
`Loader` interface (`Load(name) (string, error)`); bisql ships `RegistryLoader` (in-memory)
and `FSLoader` (`fs.FS`), plus a `LoaderFunc` adapter, and there is no default. `Parse(src,
WithLoader(l))` wires it in; `Expand(src, WithLoader(l))` returns the expanded text (for
snapshots / EXPLAIN).
Because the directive lives in a parser comment, a raw template still runs (without the
fragment) when pasted into a client.

## Expression evaluator

- `expr.Evaluator`: `Eval(expression string, scope Scope) (any, error)`.
- Default (`internal/exprlang`) wraps `github.com/expr-lang/expr`. Null literal is `nil`, but
  `x != null` also works (undefined → nil). A nil/absent `/*%if*/` is falsy; a non-nil
  non-bool is an error. A nil/absent `/*%for*/` iterable is zero iterations.
- Swappable via `bisql.WithEvaluator`.

## Dialect

Abstracts placeholder generation (MySQL `?`, PostgreSQL `$n`, Oracle `:n`, SQL Server `@pn`)
and literal formatting (for `SQLWithArgs` / `/*^ */`). bisql does not abstract SQL dialects
themselves; dialect-specific SQL (`= ANY`, `order by null`, `true`/`false`) is the author's.
