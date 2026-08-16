# CLAUDE.md — bisql development guide

Guidance for Claude Code / contributors working in this repository.

## What this is

A **2-way SQL template engine for Go**, syntax-compatible with Komapper's (Kotlin)
TEMPLATE API, adding a **first-class include** via the Komapper partial syntax
(`/*> name */`). It only converts a template into `(SQL, args)`. It is not an ORM, query
builder, or connection manager.

Read first:

1. `docs/komapper-analysis.md` — how Komapper works internally (**required**)
2. `docs/komapper-template-features.md` — full syntax inventory = the TDD backlog
3. `docs/design.md` — bisql's design decisions
4. `docs/roadmap.md` — milestones M0–M6

## Design invariants (do not break)

- **Do not parse SQL as a grammar.** Only recognize clause keywords and directives
  (shallow structural tokenization). The SQL body passes through as opaque tokens.
- **The heart of 2-way SQL is the `available` flag** (`internal/sqltmpl/render`):
  - only real tokens (Word/Other) set `available = true`;
  - a clause emits its keyword only when its body is available (drops empty clauses);
    Select/From are always kept;
  - AND/OR emits its keyword only when preceding content is available (drops dangling
    connectors);
  - blanks (Space/Eol) are buffered and flushed so removed content leaves no stray space.
- **Include = re-parse and splice.** Resolve `/*> name */` by parsing the fragment into
  nodes and splicing them in. Never implement it as raw-text embedding (that is Komapper's
  `/*# */` weakness).
- **Two layers.** Keep the SQL template layer (`internal/sqltmpl`) and the expression
  layer (`internal/exprlang` + public `pkg/expr`) separate.

## Package layout (fine-grained on purpose)

Namespace hygiene matters here. Split aggressively; prefer a small package with short
identifiers over a big package with prefixed names (e.g. `token.Word`, not `tokWord`).

```
bisql            (root) Parse / Template / Statement / Option / Loader
internal/
  sqltmpl/
    token/       Kind + kind consts
    ast/         Node + node types + Location
    lexer/       Lexer
    parser/      Parse -> ast.Node
    render/      Render (the available-flag evaluator)
  exprlang/      built-in expression evaluator (split into token/ast/lexer/parser/eval as it grows)
pkg/
  dialect/       Dialect, Placeholder, MySQL/PostgreSQL/Oracle/SQLServer
  expr/          Evaluator interface, Scope (public, so callers can plug their own)
docs/
```

- Public sub-packages live under `pkg/` to keep the top level tidy; private ones under
  `internal/`.
- The root package exposes only `Parse / Template.Build / Statement / Loader / Option`.
  Internal token/ast types must not leak into the public API.

## How to proceed

- **Lock behavior with tests per milestone** (`docs/roadmap.md` M1→M6), then move on.
- Port cases from Komapper's tests (`komapper-core/src/test/.../template/`), especially
  `TwoWayTemplateStatementBuilderTest.kt`.
- `bisql_test.go` is the spec: expected SQL/args are already written and skipped. Remove
  the `t.Skip` for a case once its milestone lands.
- Parser soundness: assert that `ast` `Text()` reproduces the input (lossless).

## Coding rules

- Prefer the standard library. Keep the default expression evaluator dependency-free; if
  an external one is ever wanted, it goes behind `bisql.WithEvaluator`, not as a hard dep.
- Public API gets English GoDoc comments.
- Before committing: `go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test ./...`.
- When a change affects behavior, add/update the corresponding test in the same commit.

## Do not

- Start full SQL grammar parsing (violates the core design).
- Implement include as raw-text embedding.
- Leak internal token/ast types through the public API.
