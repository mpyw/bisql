// Package query implements the app/query ports over a *sql.DB, building each statement from the
// embedded bisql templates (SQLite dialect). The .sql templates live under sql/, organized by
// domain; reusable fragments are underscore-prefixed (_name.sql) by convention and pulled in via
// @include. The embed uses the all: prefix so those _-prefixed files are included.
package query

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"

	"github.com/samber/lo"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
	port "github.com/mpyw/bisql/example/src/app/query"
)

//go:embed all:sql
var embedded embed.FS

// templates is the query tree rooted at sql/, so a name like "users/search.sql" resolves and its
// @include fragments resolve from the same FS.
var templates = lo.Must(fs.Sub(embedded, "sql"))

// Queries implements every app/query port over db, sharing one immutable Parser.
type Queries struct {
	db     *sql.DB
	parser *bisql.Parser
}

var (
	_ port.UserSearcher     = (*Queries)(nil)
	_ port.ActivityReporter = (*Queries)(nil)
	_ port.AuditLogWriter   = (*Queries)(nil)
	_ port.AuditLogLookup   = (*Queries)(nil)
)

// New returns a Queries backed by db.
func New(db *sql.DB) *Queries {
	return &Queries{db: db, parser: bisql.NewParser(bisql.WithDialect(dialect.SQLite))}
}

func (q *Queries) build(root string, params map[string]any) (bisql.Statement, error) {
	tmpl, err := q.parser.ParseFile(templates, root)
	if err != nil {
		return bisql.Statement{}, err
	}
	return tmpl.Build(params)
}

// rows builds root with params, runs it, and returns the result rows.
func (q *Queries) rows(ctx context.Context, root string, params map[string]any) ([]port.Row, error) {
	stmt, err := q.build(root, params)
	if err != nil {
		return nil, err
	}
	rows, err := q.db.QueryContext(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAll(rows)
}

// scanAll reads every row into a column-keyed map, so a query with a conditional column (the
// optional department in users/search) needs no special handling.
func scanAll(rows *sql.Rows) ([]port.Row, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []port.Row{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := lo.Map(vals, func(_ any, i int) any { return &vals[i] })
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, lo.SliceToMap(lo.Range(len(cols)), func(i int) (string, any) {
			if b, ok := vals[i].([]byte); ok {
				return cols[i], string(b)
			}
			return cols[i], vals[i]
		}))
	}
	return out, rows.Err()
}
