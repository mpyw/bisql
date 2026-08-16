# Komapper TEMPLATE syntax — full feature inventory (bisql target spec)

This enumerates every feature of Komapper's TEMPLATE (2-way SQL) syntax, extracted from
its source and tests (`komapper-core` / `komapper-template`). bisql aims to be
**syntax-compatible** with this. Each row is a TDD target; expected outputs are taken
verbatim from Komapper's tests so bisql can reproduce them.

Legend: **M** = milestone in `roadmap.md`.

## 1. Bind value directive `/* expr */literal` (M4)

Evaluates `expr` and emits a **bind placeholder**; the following literal is a test value
(so the raw template is runnable) and is discarded at runtime. The test value must be a
Word or a parenthesized group.

| case | template (excerpt) | value | SQL | args |
|---|---|---|---|---|
| single | `name = /*name*/'test'` | `"aaa"` | `name = ?` | `[aaa]` |
| single null | `name = /*name*/'test'` | `null` | `name = ?` | `[null]` |
| list (IN) | `name in /*name*/('a','b')` | `[x,y,z]` | `name in (?, ?, ?)` | `[x,y,z]` |
| list null | `name in /*name*/('a','b')` | `null` | `name in ?` | `[null]` |
| list empty | `name in /*name*/('a','b')` | `[]` | `name in (null)` | `[]` |
| pairs | `(name,age) in /*pairs*/(('a','b'))` | `[(x,1),(y,2)]` | `in ((?, ?), (?, ?))` | `[x,1,y,2]` |
| triples | `... in /*triples*/((...))` | `[(x,1,10),…]` | `in ((?, ?, ?), …)` | flattened |

Errors: expression empty → error; test value missing → error.

## 2. Literal value directive `/*^ expr */literal` (M4)

Evaluates `expr` and emits it as a **SQL literal** (dialect formats it). Injection-prone;
use for trusted values only.

| template | value | SQL |
|---|---|---|
| `name = /*^name*/'test'` | `"aaa"` | `name = 'aaa'` |

## 3. Embedded value directive `/*# expr */` (M4)

In **Komapper**, this emits the string value **as raw text** (not re-parsed).
If the value is empty and the surrounding clause becomes empty, the clause is auto-removed.

| template | value | SQL |
|---|---|---|
| `where age > 1 /*# orderBy */` | `"order by name"` | `where age > 1 order by name` |
| `order by /*# orderBy */` | `"name, age"` | `order by name, age` |
| `order by /*# orderBy */` | `""` | `... (order by removed)` |

> **bisql divergence:** bisql treats an embedded value as the runtime-sourced twin of a
> partial (§4): the string is **re-parsed and spliced recursively**, so directives/binds
> inside it are evaluated (`"name = /*name*/'x'"` binds `name`). Plain strings like the
> rows above behave identically. Because the text comes from a runtime value, it is an
> **injection surface** — only trusted, developer-controlled text should flow through it.

## 4. Partial directive `/*> name */` (M5) — bisql's include

References a named fragment. In Komapper this only works via `@KomapperCommand` (KSP);
the runtime builder rejects it. **bisql resolves it at runtime by re-parsing the fragment
into nodes and splicing it in**, so `/*%if*/` and binds inside the fragment work.

Errors: name empty → error; unknown fragment → error; cyclic reference → error.

## 5. Parser-level comment `/*%! ... */` (M2)

A directive-shaped comment that is **removed from output** at parse time. Its body is not
evaluated. (bisql may also let this carry build-time helpers, but the base behavior is
"disappear".)

## 6. If block `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/` (M4)

Chooses the first branch whose condition is true, else `/*%else*/`, else nothing.

| template | value | SQL |
|---|---|---|
| `where /*%if name!=null*/name = /*name*/'x'/*%end*/ and 1=1` | `name="aaa"` | `where name = ? and 1 = 1` |
| same | `name=null` | `where   1 = 1` |
| `where /*%if name!=null*/…/*%end*/ order by name` | `name=null` | `… person   order by name` (WHERE removed) |
| both clauses conditional, both false | | `select … from person` (WHERE & ORDER BY removed) |

Nesting and `if_for` combinations supported. Errors: missing `/*%end*/`; `elseif`/`else`
without `if`; empty expression.

## 7. For block `/*%for x in xs*/ … /*%end*/` (M4)

Iterates `xs` (must be iterable). Inside, these helper variables are available:

| var | meaning |
|---|---|
| `x` | current element |
| `x_index` | 0-based index |
| `x_has_next` | bool |
| `x_next_comma` | `","` unless last, else `""` |
| `x_next_and` | `"and"` unless last, else `""` |
| `x_next_or` | `"or"` unless last, else `""` |

| template | value | SQL |
|---|---|---|
| `where /*%for i in list*/age = /*i*/0 /*%if i_has_next*//*# "or"*/ /*%end*//*%end*/` | `[1,2,3]` | `where age = ? or age = ? or age = ? ` |
| `order by /*%for i in list*//*# i*//*# i_next_comma*/ /*%end*/` | `[a,b,c]` | `order by a, b, c ` |
| `/*%for i in list*/age=/*i*/0 /*# i_next_and*/ /*%end*/` | `[1,2,3]` | `age = ? and age = ? and age = ?  ` |

`in` is matched as a standalone keyword (identifiers like `index`, `line` are not split).
Errors: missing `in`; missing identifier/iterable; missing `/*%end*/`.

## 8. With block `/*%with e*/ … /*%end*/` (M4)

Expands the members of `e` into the expression scope for the block. Supports a cast:
`/*%with shape as @com.example.Circle@ */`.

| template | value | SQL |
|---|---|---|
| `/*%with item*/name=/*description*/'' and age=/*a.b.c*/0/*%end*/` | struct | binds member values |

Errors: receiver null → error; missing `/*%end*/`; `with` without matching block.

> Go note: "members" resolve to struct fields / map keys / methods via reflection.

## 9. Automatic clause removal (M4) — the heart of 2-way

A clause is emitted only if its body has real content after evaluation; otherwise the
clause keyword is dropped.

- **Auto-removable**: `WHERE`, `HAVING`, `GROUP BY`, `ORDER BY`, `FOR UPDATE`, `OPTION`
- **Always kept**: `SELECT`, `FROM` (and `FOR UPDATE` is appended verbatim in Komapper —
  verify during M4)
- A child that itself starts with a clause keyword is preserved (nesting).

## 10. Dangling AND/OR removal (M4)

`AND` / `OR` emits its keyword only when preceding content is available. So when the first
condition is stripped, the leading `AND`/`OR` is dropped.

## 11. Set operations (M2/M4)

`UNION` `EXCEPT` `MINUS` `INTERSECT` split the statement into left/right; each side is
rendered independently and joined only if both are available.

| template | SQL |
|---|---|
| `select name from a union select name from b` | unchanged |

## 12. Parentheses / subquery (M2)

`( … )` recurses into a sub-parse (`Paren` node). Empty parens `my_function()` are kept.
Subqueries `from (select …)` are preserved.

## 13. Plain comments (M2)

`--` single-line and `/* … */` multi-line (non-directive) comments pass through as text.

## 14. Expression mini-language (inside directives) (M3)

Used by `/*%if*/`, `/*%elseif*/`, `/* */`, `/*^ */`, `/*# */`, `/*%for … in HERE*/`,
`/*%with HERE*/`.

- **Literals**: `null`, `true`, `false`, string `"…"`, char `'a'`, int, long `L`,
  float `F`, double `D`, big decimal `B` (a decimal without a type suffix is an error).
- **Comparisons**: `==` `!=` `>` `<` `>=` `<=`
- **Logical**: `!` (not), `&&` (and), `||` (or)
- **Type ops**: `is @FQCN@` (type check), `as @FQCN@` (cast). RHS must be a class reference.
- **Property access**: `a.b.c`; safe call `a?.b`
- **Function calls**: `a.f(x)`, top-level/builtin `f(x)`, nested `f(g(x))`
- **Class reference**: `@com.example.Type@`
- **Comma**: argument separator. A value token starting with `is`/`as` after a comma is a
  value, not the operator.

Errors: unterminated `'`/`"`; dot not followed by property/function; illegal identifier
start; operand missing; illegal number literal; close paren missing; unsupported token.

## 15. Errors & locations (M1–M2)

Every node carries a location (line/column) used in error messages. Unterminated comment
`*/` missing → error.

---

## bisql deltas from Komapper

- Partial `/*> */` works **at runtime** and **re-parses** the fragment (§4).
- `/*# */` kept but discouraged (§3).
- Expression evaluator is **pluggable** (`bisql.WithEvaluator`); default resolves via Go
  reflection (§8, §14).
- Placeholders are **dialect-driven** (`?` / `$n` / `:n` / `@pN`).
- Output also offers a **values-embedded** form (`Statement.SQLWithArgs`) for snapshots.
- Kotlin-specific bits (inline value classes) are dropped.
