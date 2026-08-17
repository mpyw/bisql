# bisql — Design Notes

This document records the design rationale of bisql. The directive reference and the
authoring rules are maintained in the [README](../README.md); this document explains the
decisions behind them and is not a usage guide.

## Goal

bisql is a two-way SQL template engine for Go. Directives are expressed as SQL comments, so a
template is executable without modification in a SQL client, while an application converts the
same text into a parameterized statement `(SQL, Args)`. The directive syntax is inspired by
Komapper's TEMPLATE API; the runtime semantics are defined by bisql's explicit model.

## Non-goals

- **Parsing SQL as a grammar.** bisql operates at the level of directive tokenization; the SQL
  body is treated as opaque text.
- **Implicit cleanup.** bisql removes nothing it was not explicitly instructed to remove.
- **ORM, query builder, migrations, connection handling.** bisql produces only the
  transformation `template → (SQL, Args)`.

## The explicit model

Earlier iterations reproduced Komapper's `available`-flag mechanism: empty clauses were
removed, dangling `AND`/`OR` connectors were deleted, and whitespace was normalized. That
mechanism was the single largest source of implementation complexity and edge-case defects,
including placeholder numbering across nested states, dangling connectors inside parentheses,
and whitespace artifacts. bisql abandons it in favor of an explicit model.

Under the explicit model, the renderer emits the template verbatim and performs only the
following operations:

- evaluation of the `/* */` bind directive and the `/*^ */` literal directive;
- selection of a `/*%if*/` branch and iteration of `/*%for*/`;
- removal of `/*%! … */` parser comments.

All other text — clause keywords, connectors, commas, and whitespace — passes through
unchanged. The output is therefore a deterministic function of the template text and the set
of evaluated branches.

The cost of this model is an authoring obligation: the template must anchor its dynamic
fragments so that no separator is ever left dangling (`1 = 1` / `1 = 0`, a trailing sort key,
connectors placed inside `/*%if*/`, and the `/*%for … : 'sep'*/` separator clause for loops).
These obligations are specified in the README under "Authoring rules". The approach is the
established MyBatis `1 = 1` convention, applied uniformly.

The `/*%for*/` separator is the one place bisql emits build-time text that is absent from the
raw paste. It is not general raw-text emission (the removed `/*# */` embed): it is confined to
a loop's inter-iteration separator, which is exactly the construct that cannot otherwise be
made two-way — a list with no anchor position (a multi-row `VALUES` clause) has a separator in
the built output but must have none when the single raw body is pasted into a client. Crucially
the separator is a **constant string literal parsed at build time, not an expression**: were it
an evaluated expression, a runtime value could be emitted verbatim, which is precisely the
raw-text-injection surface the design excludes. It is single- or double-quoted with the quote
doubled to escape; its introducing colon is the first one not inside quotes, `()`, `[]`, or
`{}`, so it does not collide with a ternary/slice/map colon in the iterable expression.

Three consequences follow directly from verbatim emission:

- **No clause tokenization.** The lexer does not recognize `select`, `where`, `and`, `union`,
  or any other keyword; these are ordinary words. The clause, connector, and set-operation
  apparatus is absent entirely.
- **Global placeholder numbering.** Placeholder indices are produced by a single
  renderer-global counter. Binds in unselected branches and unreached loop iterations are
  never visited, so numbering is contiguous for the `$n`, `:n`, and `@pn` forms.
- **Whitespace fidelity.** Whitespace is preserved exactly as written, including blank lines.

## Rendering output

A single render pass produces the parameterized `SQL` and the `Args` slice. It does not build
the values-embedded rendering eagerly; instead, it records the byte range of each placeholder
within `SQL`. `Statement.SQLWithArgs` reconstructs the values-embedded form on demand by
splicing the arguments into those ranges. Consequently, a statement that is built and executed
but never inspected does not incur the cost of literal formatting. The recorded ranges remain
aligned with `Args` because a placeholder and its argument are appended together during
rendering.

## Directives

| Syntax                                              | Meaning                                                              |
|:----------------------------------------------------|:--------------------------------------------------------------------|
| `/* expr */literal`                                 | Bind placeholder; `literal` is the two-way test value.              |
| `/*^ expr */literal`                                | Inline SQL literal (dialect-formatted; injection-prone).            |
| `/*%if e*/ … /*%elseif e*/ … /*%else*/ … /*%end*/`  | Conditional selection of a branch.                                  |
| `/*%for x in xs*/ … /*%end*/`                       | Iteration; an optional `: 'sep'` clause emits a separator between iterations. |
| `/*%! … */`                                         | Parser comment (removed); also hosts `@include`.                    |
| `/*%! @include name */`                             | Preprocessor directive; splices a static fragment.                  |

The following constructs from earlier iterations and from Komapper have been removed:
`/*> name */` (partial), `/*# expr */` (embedded), and `/*%with e*/`. A comment of the form
`/*# … */` is now an ordinary comment. Composition is provided exclusively by `@include`;
there is no raw-text substitution. `/*%with*/` was removed as redundant: because the
expression language already supports qualified access, `/*criteria.min*/0` expresses what
`/*%with criteria*/ /*min*/0 /*%end*/` previously expressed.

### Bind expansion by test-literal shape

The shape of the test literal, determined from the template text, selects the expansion
strategy:

- A parenthesized test `(...)` expands an iterable into a placeholder list. An empty iterable
  renders as `(null)`; a slice of slices renders as row tuples.
- A scalar test binds the value as a single parameter. A slice therefore becomes one array
  parameter, which is the form required by the PostgreSQL `= ANY($1::type[])` construct.
  Whether the target dialect and driver support array binding is outside bisql's scope; in
  practice this applies to PostgreSQL.

## `@include` preprocessing

The `internal/sqltmpl/preprocess` package runs before lexing. It scans the raw text — skipping
string spans, quoted-identifier spans, and `--` line comments — for `/*%! @include name */`
and replaces each occurrence with the resolved fragment text. Resolution is recursive and is
bounded by cycle detection and a maximum depth. The result is a fully expanded template that
remains two-way.

Fragment resolution is delegated to the `Loader` interface:

```go
type Loader interface {
	Load(name string) (string, error)
}
```

bisql provides `RegistryLoader` (in-memory), `FSLoader` (over an `fs.FS`), a `LoaderFunc`
adapter, and `StackedLoader`, which chains loaders and falls through to the next whenever one
reports the fragment is not found (`errors.Is(err, ErrNotFound)`, or `fs.ErrNotExist`); any
other error aborts the lookup. There is no default loader. `Parse` wires the configured loader into the preprocessor,
and `Expand` returns the expanded text for snapshots and pre-execution inspection. `ParseFile`
and `ExpandFile` read the root template from an `fs.FS` and, when no loader is configured
explicitly, default the include loader to an `FSLoader` over that same `fs.FS`, so that the
root template and its fragments reside in one file tree. Because the directive is carried on
the parser-comment channel, a template that references a fragment still executes verbatim in a
client, without the fragment.

## Expression evaluation

The expression layer is separated from the template layer behind the `expr.Evaluator`
interface:

```go
type Evaluator interface {
	Eval(expression string, scope Scope) (any, error)
}
```

The default implementation (`internal/exprlang`) wraps `github.com/expr-lang/expr`. The null
literal is `nil`; the idiom `x != null` is also accepted because an undefined identifier
resolves to nil. A nil or absent `/*%if*/` condition evaluates as false, and a non-nil,
non-boolean condition is an error. A nil or absent `/*%for*/` iterable yields zero iterations.
The evaluator is replaceable through `bisql.WithEvaluator`.

## Dialect

The `dialect` package abstracts placeholder generation (`?` for MySQL, `$n` for PostgreSQL,
`:n` for Oracle, `@pn` for SQL Server) and literal formatting (used by `SQLWithArgs` and the
`/*^ */` directive). bisql does not abstract SQL dialects themselves; dialect-specific SQL such
as `= ANY`, `order by null`, and the `true`/`false` literals is the author's responsibility.
