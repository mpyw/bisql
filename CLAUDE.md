# CLAUDE.md — bisql development guide

Guidance for Claude Code / contributors working in this repository.

## What this is

A **2-way SQL template engine for Go**. Directives are SQL comments, so a template runs
as-is in a client; an application converts it to `(SQL, args)`. Syntax is inspired by
Komapper's TEMPLATE API, but the **semantics are the explicit model** (below) — not a
faithful Komapper port. Not an ORM, query builder, or connection manager.

Read `docs/design.md` (the design rationale) and the README (directive reference +
authoring rules).

## Design invariants (do not break)

- **Do not parse SQL as a grammar.** The lexer recognizes only directive comments, plain
  comments, string/identifier quotes (`'` `"` `` ` ``), and parentheses. Everything else —
  including former keywords like `select`/`where`/`and`/`union` — is opaque `Word`/`Other`.
- **Remove nothing implicitly.** The renderer emits text **verbatim**; it only evaluates
  directives and drops `/*%! ... */` parser comments. No empty-clause removal, no dangling
  `AND`/`OR` cleanup, no whitespace normalization. Predictability over magic. The author
  anchors dynamic SQL (see Authoring rules).
- **Placeholder numbering is a single renderer-global counter** (`renderer.nargs`), not a
  per-state length — binds in unrendered branches/loops are never counted, so numbering is
  gap-free across every dialect (`$n`/`:n`/`@pn`).
- **`SQLWithArgs` is derived lazily.** Render produces `SQL`, `Args`, and each placeholder's
  byte range (`Result.ArgSpans`, aligned with `Args`); `Statement.SQLWithArgs()` splices
  literals into those ranges on demand. The renderer does not build a values-embedded buffer.
- **IN-expansion is keyed on the bind's test-literal shape**, not the value type: a `(...)`
  test expands to a placeholder list; a scalar test binds the value as-is (a slice becomes
  one array parameter, for Postgres `= ANY`).
- **`@include` is a text preprocessor** (`internal/sqltmpl/preprocess`), run before lexing:
  recursive, cycle-detected, string/comment-aware. It is the *only* composition mechanism
  (there is no raw-text embed / partial directive anymore).
- **Two layers.** Keep the SQL template layer (`internal/sqltmpl`) and the expression layer
  (`internal/exprlang` + public `expr`) separate.

## Authoring rules (mirror of README — keep in sync)

The engine cleans nothing, so templates must anchor:

- WHERE/HAVING: `1 = 1` / `1 = 0` anchor; conditions carry a leading `and`/`or`.
- ORDER BY: trailing stable key (`id`). SELECT/SET lists: base anchor + leading comma, or a
  `/*%for*/` + `/*%if x_has_next*/,/*%end*/` (there is **no** `_next_comma` helper).
- JOIN/UNION: put the connector inside the `/*%if*/`.
- Escape quotes by **doubling** (`''` `""` `` `` ``); backslash escapes are not recognized.
- `/* ... */` is a bind directive → a plain comment must start with a non-identifier char
  (`/** ... */`); `/*%! ... */` is a stripped parser comment.
- Dynamic identifiers = whitelisted `/*%if*/` toggles (no raw-text substitution).
- Portability: `1=1` universal; `true`/`false`, `= ANY(array)`, `order by null` are
  dialect-specific.

## Package layout (fine-grained on purpose)

```
bisql            (root) NewParser / Parser / Parse / Expand / Template / Statement / Option
                 include: Loader interface + RegistryLoader / FSLoader / LoaderFunc / WithLoader
dialect/         Dialect, Placeholder, Literal, MySQL/PostgreSQL/Oracle/SQLServer
expr/            Evaluator interface, Scope (public: callers can plug their own)
internal/
  sqltmpl/
    token/       Kind + kind consts
    ast/         Node + node types + Location
    lexer/       Lexer (directive scanner over opaque text)
    parser/      Parse -> ast.Node (reducer-stack for blocks + bind test literals)
    render/      Render (verbatim emit; the heart of the explicit model)
    preprocess/  @include text preprocessor
  exprlang/      default evaluator (wraps github.com/expr-lang/expr)
```

Public sub-packages sit at the top level (`dialect`, `expr`) — no `pkg/`. Internal
token/ast types must not leak through the public API.

## Coding rules

- Prefer the standard library. The one deliberate dependency is `github.com/expr-lang/expr`
  (the default evaluator); alternatives go behind `bisql.WithEvaluator`. Tests may use
  `github.com/jackc/pgx/v5` under the `integration` build tag only.
- Tooling is pinned in `mise.toml`. Before committing run `mise run check` (fmt + build +
  vet + golangci-lint + deadcode + `go test -race`); it must be clean.
- When a change affects behavior, add/update the corresponding test in the same commit.
- e2e goldens live under `testdata/e2e/`; regenerate with `go test -run TestE2E -update`.

## Do not

- Start full SQL grammar parsing, or reintroduce implicit clause/connector removal.
- Add a raw-text embed / partial directive (composition is `@include` only).
- Leak internal token/ast types through the public API.
