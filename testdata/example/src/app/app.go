// Package app is the application core. App aggregates the query ports — received via manual
// dependency injection in cmd/serve — behind the methods the presentation layer calls. The logic
// here is deliberately thin: it is the seam where validation, authorization, and orchestration
// would live.
package app

import (
	"context"

	"github.com/mpyw/bisql/example/src/app/query"
)

// App is the application service, wiring the query ports behind request methods.
type App struct {
	users    query.UserSearcher
	activity query.ActivityReporter
	writer   query.AuditLogWriter
	lookup   query.AuditLogLookup
}

// New wires the query ports into an App (manual DI).
func New(users query.UserSearcher, activity query.ActivityReporter, writer query.AuditLogWriter, lookup query.AuditLogLookup) *App {
	return &App{users: users, activity: activity, writer: writer, lookup: lookup}
}

func (a *App) SearchUsers(ctx context.Context, in query.SearchUsersInput) ([]query.Row, error) {
	return a.users.SearchUsers(ctx, in)
}

func (a *App) ReportActivity(ctx context.Context, in query.ActivityReportInput) ([]query.Row, error) {
	return a.activity.ReportActivity(ctx, in)
}

func (a *App) AppendAuditLogs(ctx context.Context, events []query.AuditEvent) (int64, error) {
	return a.writer.AppendAuditLogs(ctx, events)
}

func (a *App) LookupAuditLogs(ctx context.Context, keys []query.AuditKey) ([]query.Row, error) {
	return a.lookup.LookupAuditLogs(ctx, keys)
}
