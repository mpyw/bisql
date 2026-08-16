// Package bisql is a 2-way SQL template engine for Go.
//
// 2-way SQL writes directives as SQL comments, so a template can be pasted into a SQL
// client and run as-is, while an application can toggle conditions, iterate, and reuse
// fragments. bisql is inspired by Komapper's (Kotlin) TEMPLATE API and adds a first-class
// include directive via the Komapper-compatible partial syntax (/*> name */).
//
// bisql does not parse SQL as a grammar. It performs a shallow structural tokenization
// that recognizes clause keywords and directives, and drops clauses that become empty and
// AND/OR left dangling. See docs/ for the design.
package bisql
