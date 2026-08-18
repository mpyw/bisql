// Package query defines the query ports the application depends on: one interface per query,
// with its input and output types. The implementations live in app/infrastructure/query.
package query

import "context"

// Row is one result row, keyed by column name.
type Row = map[string]any

// SearchUsersInput holds the optional filters of the user search; a zero-valued field is omitted
// from the query.
type SearchUsersInput struct {
	Query          string
	AgeBand        string
	Sort           string
	WithDepartment bool
	ActiveOnly     bool
	Department     int
	Limit          int
	DepartmentIDs  []int
	Tags           []string
	EmailDomains   []string
}

// UserSearcher runs the dynamic user search.
type UserSearcher interface {
	SearchUsers(ctx context.Context, in SearchUsersInput) ([]Row, error)
}

// ActivityReportInput bounds the report to an optional date window and action set.
type ActivityReportInput struct {
	Since   string
	Until   string
	Actions []string
}

// ActivityReporter aggregates audit events per department.
type ActivityReporter interface {
	ReportActivity(ctx context.Context, in ActivityReportInput) ([]Row, error)
}

// AuditEvent is one row appended by AppendAuditLogs.
type AuditEvent struct {
	UserID int
	Action string
}

// AuditLogWriter appends a batch of audit events and returns the number inserted.
type AuditLogWriter interface {
	AppendAuditLogs(ctx context.Context, events []AuditEvent) (int64, error)
}

// AuditKey is a composite (user_id, action) lookup key.
type AuditKey struct {
	UserID int
	Action string
}

// AuditLogLookup fetches audit rows for a set of composite keys.
type AuditLogLookup interface {
	LookupAuditLogs(ctx context.Context, keys []AuditKey) ([]Row, error)
}
