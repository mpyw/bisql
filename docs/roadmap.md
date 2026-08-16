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

## M3: default expression evaluator

- [ ] `internal/exprlang`: comparisons, logical ops, literals, property/method, safe call
- [ ] resolve map / struct / methods via reflection
- [ ] split into token/ast/lexer/parser/eval sub-packages as it grows
- [ ] `exprlang_test.go`

## M4: renderer (heart of 2-way)

- [ ] `internal/sqltmpl/render`: the available-flag evaluator
- [ ] empty-clause removal, dangling AND/OR removal, blank normalization
- [ ] bind IN expansion (list / tuple / empty -> `(null)`)
- [ ] for-loop helper variables
- [ ] dialect placeholders
- [ ] unskip the M4 cases in `bisql_test.go` (WHERE removal, dangling AND, IN, if/for)

## M5: include (partial)

- [ ] `include.go`: `Loader` (Register / LoadFS)
- [ ] resolve `/*> name */` by re-parsing and splicing
- [ ] detect cyclic references and missing fragments
- [ ] unskip `TestPartialInclude` (fragment-internal `/*%if*/` and binds must work)

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
