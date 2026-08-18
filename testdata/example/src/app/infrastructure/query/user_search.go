package query

import (
	"context"

	"github.com/samber/lo"

	port "github.com/mpyw/bisql/example/src/app/query"
)

// SearchUsers maps the input to the optional predicates of users/search.sql. Each supplied field
// becomes one conjunction off the query's 1 = 1 anchor; the absent ones drop out of the SQL.
func (q *Queries) SearchUsers(ctx context.Context, in port.SearchUsersInput) ([]port.Row, error) {
	params := lo.OmitByValues(map[string]any{
		"q":       in.Query,
		"ageBand": in.AgeBand,
		"sortKey": in.Sort,
	}, []any{""})
	if in.WithDepartment {
		params["withDepartment"] = true
	}
	if in.ActiveOnly {
		params["activeOnly"], params["status"] = true, "active"
	}
	if in.Department != 0 {
		params["departmentId"] = in.Department
	}
	if in.Limit != 0 {
		params["limit"] = in.Limit
	}
	if len(in.DepartmentIDs) > 0 {
		params["departmentIds"] = lo.ToAnySlice(in.DepartmentIDs)
	}
	if len(in.Tags) > 0 {
		params["tags"] = lo.ToAnySlice(in.Tags)
	}
	if len(in.EmailDomains) > 0 {
		params["emailDomains"] = lo.ToAnySlice(in.EmailDomains)
	}
	return q.rows(ctx, "users/search.sql", params)
}
