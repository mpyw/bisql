---
name: advance-milestone
description: Implement the next bisql milestone (M1 lexer, M2 parser, M3 exprlang, M4 renderer, M5 include) test-first, porting cases from Komapper and keeping the design invariants. Use when asked to "implement M<n>", "work on the lexer/parser/renderer/include", or "advance bisql".
---

# Advance a bisql milestone (TDD)

bisql is built milestone by milestone (see `docs/roadmap.md`). This skill drives one
milestone test-first without breaking the design invariants.

## Before writing code

1. Read `CLAUDE.md` (invariants) and `docs/komapper-template-features.md` (the spec with
   expected outputs). For internals, read `docs/komapper-analysis.md`.
2. Identify the milestone and its package:
   - M1 → `internal/sqltmpl/lexer`
   - M2 → `internal/sqltmpl/parser`
   - M3 → `internal/exprlang`
   - M4 → `internal/sqltmpl/render` (heart of 2-way)
   - M5 → `include.go` + `Loader`
3. Find the matching Komapper tests to port
   (`komapper-core/src/test/.../template/`, `komapper-template/.../*Test.kt`). Their
   assertions are the source of truth for expected SQL/args.

## The loop

1. **Write/enable tests first.** For M4/M5, remove the `t.Skip` from the relevant cases in
   `bisql_test.go`. For M1–M3, add a package-local `_test.go` with cases ported from
   Komapper. Run `go test ./...` and confirm they fail for the right reason.
2. **Implement** the smallest change to pass. Keep the invariants:
   - never parse SQL as a grammar (shallow tokenization only);
   - the renderer's empty-clause / dangling-AND-OR removal is the `available` flag;
   - include = re-parse the fragment and splice (never raw-text embed);
   - keep the two layers (sqltmpl vs exprlang) separate;
   - don't leak internal token/ast types through the public API.
3. **Parser soundness (M2):** assert `ast` node `Text()` reproduces the input (lossless).
4. **Verify** before finishing:
   ```sh
   go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test ./...
   ```
5. **Commit** with the behavior change and its tests together. End the commit message with
   the Co-Authored-By trailer used in this repo.

## Porting notes

- Komapper defaults to lowercase keywords in tests; bisql passes SQL through verbatim, so
  match the input's case.
- bisql defaults to the MySQL dialect (`?`). When porting a case that shows `?`, it maps
  directly; for other dialects set `bisql.WithDialect`.
- IN expansion: list → `(?, ?, …)`, empty list → `(null)`, nil → single `?`.
- for-loop helpers: `x_index`, `x_has_next`, `x_next_comma`, `x_next_and`, `x_next_or`.

## Definition of done for the milestone

- All cases for that milestone in `bisql_test.go` (and package-local tests) pass.
- `go vet` clean, `gofmt` clean.
- `docs/roadmap.md` checkboxes for the milestone are ticked in the same commit.
