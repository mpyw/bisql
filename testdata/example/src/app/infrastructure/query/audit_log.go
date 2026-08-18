package query

import (
	"context"

	"github.com/samber/lo"

	port "github.com/mpyw/bisql/example/src/app/query"
)

// AppendAuditLogs inserts a batch of events with one round trip (bulk-insert.sql: a zero-row
// seed plus one `union all select` per event).
func (q *Queries) AppendAuditLogs(ctx context.Context, events []port.AuditEvent) (int64, error) {
	rows := lo.Map(events, func(e port.AuditEvent, _ int) any {
		return map[string]any{"userId": e.UserID, "action": e.Action}
	})
	stmt, err := q.build("audit_logs/bulk-insert.sql", map[string]any{"events": rows})
	if err != nil {
		return 0, err
	}
	res, err := q.db.ExecContext(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LookupAuditLogs fetches rows for a set of composite (user_id, action) keys (find-by-keys.sql:
// a row-value IN over a union-all subquery).
func (q *Queries) LookupAuditLogs(ctx context.Context, keys []port.AuditKey) ([]port.Row, error) {
	rows := lo.Map(keys, func(k port.AuditKey, _ int) any {
		return map[string]any{"userId": k.UserID, "action": k.Action}
	})
	return q.rows(ctx, "audit_logs/find-by-keys.sql", map[string]any{"keys": rows})
}
