// Package bisql is a two-way SQL template engine for Go.
//
// Directives are written as SQL comments, so a template is simultaneously a valid SQL
// statement — it can be pasted into a client and run as-is — while an application converts the
// same text into a parameterized statement (SQL, Args). The directive syntax is inspired by
// Komapper's TEMPLATE API.
//
// bisql follows an explicit model: the renderer emits the template verbatim, evaluating only the
// bind, literal, conditional, iteration, and @include directives and stripping parser comments.
// It removes nothing implicitly — no empty-clause removal, no dangling AND/OR cleanup, no
// whitespace normalization — so the author anchors every dynamic fragment (a 1 = 1 predicate, a
// leading connector) to keep the rendered SQL valid. See the README for the directive reference
// and authoring rules.
package bisql
