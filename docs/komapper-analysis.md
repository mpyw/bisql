# Analysis of Komapper's template implementation

bisql builds on Komapper's TEMPLATE API (2-way SQL). This records what the Komapper
source (`komapper/komapper`) actually does. Scope: `komapper-core/.../template/` and
`komapper-template/`.

## Conclusion: it does NOT parse SQL as a grammar

Komapper never interprets the SQL's meaning (expressions, functions, joins, columns). It
does a **shallow structural tokenization**: it recognizes only the items below and passes
everything else through as opaque tokens (`WORD` / `OTHER` / `SPACE` / `EOL`).

Recognized by `SqlTokenizer`:

- top-level clause keywords: `SELECT FROM WHERE GROUP BY HAVING ORDER BY FOR UPDATE OPTION`
- logical operators: `AND` `OR`
- set operators: `UNION` `EXCEPT` `MINUS` `INTERSECT`
- parentheses `(` `)`, string literals `'...'` (with `''` escaping)
- line comments `--`, block comments `/* */`
- directive comments (below)

**Why recognize only clauses?** Solely to implement the crux of 2-way SQL:

> when a `/*%if*/` empties a clause, **drop the clause keyword** (remove an empty `WHERE`),
> and drop a leading `AND` / `OR` left dangling.

Knowing clause boundaries is enough; the clause body need not be understood.

## Two layers

The template is two independent little languages:

1. **SQL template layer** (`template/sql/`): `SqlTokenizer` → `SqlParser` → `SqlNode`
   (a shallow structural tree). Handles directives and the clause skeleton.
2. **Expression layer** (`template/expression/`): `ExprTokenizer` → `ExprParser` →
   `ExprNode` → `ExprEvaluator`. A small language for the contents of `/*%if HERE*/`,
   `/* HERE */`, etc.

bisql keeps this split (`internal/sqltmpl` and `internal/exprlang` + public `expr`).

## Directives

`SqlTokenizer` branches on the character right after `/*`.

| syntax | kind | meaning |
|---|---|---|
| `/* expr */literal` | bind value | evaluate expr, emit a **bind placeholder**; the following Word or `(...)` is a 2-way test value |
| `/*^ expr */literal` | literal value | evaluate expr, emit as a **SQL literal** (dialect formats it) |
| `/*# expr */` | embedded value | emit the string **as raw text**; **not re-parsed** (see below) |
| `/*> name */` | partial | partial reference; **not supported by the runtime builder** (see below) |
| `/*%! ... */` | parser-level comment | removed from output |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` | conditional | |
| `/*%for x in xs*/ … /*%end*/` | iteration | supplies `x_index x_has_next x_next_comma x_next_and x_next_or` |
| `/*%with e*/ … /*%end*/` | expand receiver members into the expression scope | |

Unknown directive names raise `SqlException("Unsupported directive name ...")`.

### Why `/*# */` (embedded) is not re-parsed

`SqlParser.parseEmbeddedValueDirective` only extracts the expression string and pushes a
leaf `SqlNode.EmbeddedValueDirective`. At render time the builder appends `obj.toString()`
verbatim. So the injected text goes through neither the SQL tokenizer nor expression
evaluation.

→ The failure mode described in the Zenn article ("passing a fragment with `/*%if*/` into
`/*# */` leaves the directive as a comment and silently misbehaves") comes from exactly
this "append raw text as a leaf". **bisql's include splits from this**: it re-parses the
fragment into nodes and splices it into the tree.

### `/*> */` (partial) is not supported at runtime

`SqlNode.PartialDirective` exists as a type, but
`TwoWayTemplateStatementBuilder.visit` rejects it:

```kotlin
is SqlNode.PartialDirective -> {
    error("PartialDirective \"${node.token}\" is not supported in this builder. Use @KomapperCommand.")
}
```

Partials work only in the KSP code-generation path (`@KomapperCommand`). **bisql supports
partials at runtime**, which is its main differentiator from Komapper.

## Parser: folding clauses with a reducer stack

`SqlParser` is not recursive descent but a **reducer stack**:

- for each token, push a `Reducer` for clause/operator keywords, else `addNode` to the
  current top reducer.
- `SELECT` → push `SelectReducer`, `WHERE` → `WhereReducer`, `AND` → `AndReducer`, …
- on `(`, start a **recursive sub-parser** to build a `Paren` node; return on `)`.
- `UNION`/`EXCEPT`/… fold everything so far via `reduceAll` into the left side, then push
  a `SetReducer`.
- `/*%end*/` folds down to the nearest `BlockReducer` (if/for/with).
- finally `reduceAll()` folds all reducers into one `SqlNode.Statement`.

Node kinds (see the Go port in `internal/sqltmpl/ast`):

- `Statement`, `Set(keyword,left,right)`, `Paren(node)`
- `Clause.{Select,From,Where,Having,GroupBy,OrderBy,ForUpdate,Option}`
- `BiLogicalOp.{And,Or}`
- `IfBlock / ForBlock / WithBlock` and their `*Directive`s
- `BindValueDirective / LiteralValueDirective / EmbeddedValueDirective / PartialDirective`
- leaves: `Token.{Word,Space,Eol,Comment,Other}`

## Rendering: the `available` flag is the heart of 2-way

`TwoWayTemplateStatementBuilder.visit(state, node)` walks the tree and assembles
`(SQL string, bind values)`. `State.available: Boolean` is the key.

- appending a **Word / Other** sets `available = true` ("there is real SQL body"). Blanks
  (space/eol) do not.
- a **clause** (Where/Having/GroupBy/OrderBy/Option) folds its children in a fresh State
  and emits `keyword + child` only if `childState.available`; otherwise **the whole clause
  disappears**.
  - exceptions: `Select` / `From` / `ForUpdate` are always emitted.
  - `startsWithClause()`: if the child begins with a clause keyword (nesting), keep it.
- **`AND` / `OR`** emit their keyword only when `state.available` is already true → a
  leading `AND`/`OR` after a stripped condition **is dropped**.
- **blank handling** matters: blanks are buffered and flushed right before a real token or
  bind, so removed conditions do not leave stray whitespace. Multiple EOLs are normalized
  to keep just the last (`convertBlankNodesToString`).

### Binding and IN expansion

When a `BindValueDirective` evaluates to

- an `Iterable` → expand to `(?, ?, …)`; elements that are `Pair`/`Triple` become
  `((?,?), …)` (composite-key IN); empty → `(null)`.
- otherwise → a single `?` (the test literal is skipped).

Kotlin inline value classes are unwrapped before binding (`rebuildValue`) — not relevant
for Go.

### for loop

`ForBlock` iterates the (Iterable) expression, exposing `x`, `x_index`, `x_has_next`,
`x_next_comma` (`,` unless last), `x_next_and`, `x_next_or` in the expression scope so you
can write separators yourself.

## Expression mini-language (the contents of `/*%if*/` etc.)

Per `ExprTokenType`:

- literals: char / string / int / long / float / double / big decimal / null / true / false
- logical `!` `&&` `||`; comparisons `== != > < >= <=`
- `is` (type check), `as` (cast), class refs `@FQCN@`, `?.` (safe call)
- property access / function calls (members resolved by reflection), `,`, parentheses

A small evaluator over a value map. The Go version resolves struct fields / map keys /
methods by reflection. **The evaluator is an interface** (swappable) with a default impl
(`internal/exprlang`, public interface in `expr`).

## What bisql ports vs. changes

Faithful port:

- SqlTokenType / SqlNode model
- reducer-stack parser
- the `available` flag for empty-clause and dangling AND/OR removal (**the value of 2-way**)
- blank buffering / whitespace normalization
- bind IN expansion, for-loop helper variables

bisql's choices:

- **include at runtime**: resolve `/*> name */` (Komapper partial syntax) by **re-parsing
  the fragment into nodes and splicing it** (unlike `/*# */` raw embed).
- keep `/*# */` but **discourage** it (compatibility / escape hatch).
- **pluggable** expression evaluator with a default.
- **dialect-driven** placeholders (`?` / `$n` / `:n` / `@pN`).
- drop Kotlin-specific bits (value classes).
- output a `struct { SQL string; Args []any }` plus a values-embedded form
  (`SQLWithArgs`) for the snapshot workflow.
