package query

import (
	"context"

	"github.com/samber/lo"

	port "github.com/mpyw/bisql/example/src/app/query"
)

// ReportActivity aggregates audit events per department over an optional date window.
func (q *Queries) ReportActivity(ctx context.Context, in port.ActivityReportInput) ([]port.Row, error) {
	params := lo.OmitByValues(map[string]any{
		"since": in.Since,
		"until": in.Until,
	}, []any{""})
	if len(in.Actions) > 0 {
		params["actions"] = lo.ToAnySlice(in.Actions)
	}
	return q.rows(ctx, "audit_logs/activity-report.sql", params)
}
