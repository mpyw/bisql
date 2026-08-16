# bisql roadmap

Build up milestone by milestone, locking behavior with tests. Each milestone keeps
`go test ./...` green before moving on.

## M0: scaffolding (this commit)

- [x] go.mod / LICENSE / .gitignore / README / docs
- [x] Komapper analysis + full feature inventory
- [x] token / ast model (`internal/sqltmpl`)
- [x] public API types and signatures (compile-clean, bodies return not-implemented)
- [x] TDD spec (`bisql_test.go`, skipped per milestone)

## M1: lexer ✅

- [x] `internal/sqltmpl/lexer`
- [x] branch right after `/*` to detect directive kinds
- [x] recognize clause keywords / AND / OR / set operators / parens / string literals
- [x] `lexer_test.go` (ported from Komapper's `SqlTokenizerTest`)

Note: added `token.Illegal` + `Lexer.Err()` for lexing errors; `Location` reports the
start of each token (bisql convention; Komapper reports the post-consume position).

## M2: parser ✅

- [x] `internal/sqltmpl/parser` (reducer-stack strategy)
- [x] fold clauses/operators, recurse into `(...)`, set operations, if/for/with blocks
- [x] `parser_test.go` (asserts `ast.Text()` reproduces the input = lossless; ported from `SqlParserTest`)

## M3: default expression evaluator ✅

Decision (revised): rather than maintain a bespoke expression parser, the default
evaluator wraps **`github.com/expr-lang/expr`** (the de-facto Go expression language:
safe, bytecode-compiled, rich — `in`, `len`, optional chaining `a?.b`, nil-coalescing).
The hand-rolled lexer/eval from the first M3 pass were deleted.

- [x] `internal/exprlang`: thin wrapper over expr-lang, compile-cached per expression
- [x] `expr.Evaluator` seam kept — custom/goja backends remain droppable via `WithEvaluator`
- [x] `exprlang_test.go` (wrapper contract: scope binding, Go-value passthrough, errors)

Notes: the null literal is written `nil` (expr-lang's spelling), but Komapper's
`x != null` idiom still works unchanged — `null` is an undefined identifier, and
`AllowUndefinedVariables` resolves undefined identifiers to nil. Expressions live inside
SQL comments and never reach the DB, so this has no bearing on the 2-way property.
`exprlang` stays a single package: its types never cross a package boundary, so the
fine-grained split that pays off in `sqltmpl` (shared `token`/`ast` vocab) would be
export ceremony here.

## M4: renderer (heart of 2-way) ✅

- [x] `internal/sqltmpl/render`: the available-flag evaluator
- [x] empty-clause removal, dangling AND/OR removal, blank normalization (keep-from-last-EOL)
- [x] bind IN expansion (list / tuple / empty -> `(null)`)
- [x] for-loop helper variables (`_index`/`_has_next`/`_next_comma`/`_next_and`/`_next_or`)
- [x] dialect placeholders + literal formatting (`dialect.FormatLiteral`)
- [x] unskip the M4 cases in `bisql_test.go` (WHERE removal, dangling AND, IN, if/for) — all green

Ported the algorithm directly from Komapper's `TwoWayTemplateStatementBuilder`: clauses
nest (ORDER BY becomes a child of WHERE), and a dropped clause keeps a nested clause's
text via `startsWithClause()`, which is what preserves the surrounding whitespace.

## M5: include (partial) + recursive embedded ✅

Design decision: expansion is **recursive** for both `/*> name */` (partial) and
`/*# expr */` (embedded value), and they are unified on one splice path — differing only
in source (static loader-registered fragment vs. runtime scope expression). See the
"Expansion design" section in [`design.md`](./design.md).

- [x] `include.go`: `Loader` (Register / LoadFS), fragment parse-cache, resolver wiring
- [x] resolve `/*> name */` by re-parsing and splicing (recursive; nested partials work)
- [x] `/*# expr */` re-parses its runtime string and splices recursively (extends Komapper)
- [x] shared depth guard (`render.DefaultMaxDepth`) + partial-name cycle detection
- [x] unskip `TestPartialInclude`; add `TestPartialRecursive`, `TestPartialCycle`,
      `TestEmbeddedRecursive`

## M6: finishing

- [ ] `Statement.SQLWithArgs` (values-embedded form)
- [ ] optional ahead-of-time expander (`cmd/bisql-expand`)
- [ ] benchmarks; confirm `Template` immutability / concurrency
- [ ] GoDoc examples (`example_test.go`), CI (`go test` / `go vet` / `golangci-lint`)

## Source cases to port from Komapper

`komapper-core/src/test/.../template/`:

- `sql/SqlTokenizerTest.kt`, `sql/SqlParserTest.kt`
- `expression/ExprTokenizerTest.kt`, `expression/ExprParserTest.kt`
- `TwoWayTemplateStatementBuilderTest.kt` (rich source of 2-way behavior)
- `ExprEvaluatorTest.kt` (expression semantics)
