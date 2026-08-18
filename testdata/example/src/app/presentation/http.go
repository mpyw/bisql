// Package presentation is the HTTP delivery layer. Handlers parse a request into an app input,
// call the application core, and render JSON. It depends only on *app.App (injected).
package presentation

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/mpyw/bisql/example/src/app"
	"github.com/mpyw/bisql/example/src/app/query"
)

// Handler serves the API over the application core.
type Handler struct {
	app *app.App
}

// New returns a Handler backed by the application core (manual DI).
func New(a *app.App) *Handler {
	return &Handler{app: a}
}

// Routes returns the API handler.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", h.searchUsers)
	mux.HandleFunc("GET /reports/activity", h.activityReport)
	mux.HandleFunc("POST /audit-logs/batch", h.appendAuditLogs)
	mux.HandleFunc("GET /audit-logs/lookup", h.lookupAuditLogs)
	return mux
}

func (h *Handler) searchUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := h.app.SearchUsers(r.Context(), query.SearchUsersInput{
		Query:          q.Get("q"),
		AgeBand:        q.Get("age_band"),
		Sort:           q.Get("sort"),
		WithDepartment: q.Get("with_department") == "true",
		ActiveOnly:     q.Get("active_only") == "true",
		Department:     atoi(q.Get("department")),
		Limit:          atoi(q.Get("limit")),
		DepartmentIDs:  lo.Map(csv(q.Get("department_ids")), func(s string, _ int) int { return atoi(s) }),
		Tags:           csv(q.Get("tags")),
		EmailDomains:   q["email_domain"],
	})
	writeRows(w, rows, err)
}

func (h *Handler) activityReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := h.app.ReportActivity(r.Context(), query.ActivityReportInput{
		Since:   q.Get("since"),
		Until:   q.Get("until"),
		Actions: csv(q.Get("actions")),
	})
	writeRows(w, rows, err)
}

type auditEventReq struct {
	UserID int    `json:"userId"`
	Action string `json:"action"`
}

func (h *Handler) appendAuditLogs(w http.ResponseWriter, r *http.Request) {
	var body []auditEventReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	events := lo.Map(body, func(e auditEventReq, _ int) query.AuditEvent {
		return query.AuditEvent{UserID: e.UserID, Action: e.Action}
	})
	n, err := h.app.AppendAuditLogs(r.Context(), events)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"inserted": n})
}

func (h *Handler) lookupAuditLogs(w http.ResponseWriter, r *http.Request) {
	keys := lo.FilterMap(r.URL.Query()["key"], func(kv string, _ int) (query.AuditKey, bool) {
		userID, action, ok := strings.Cut(kv, ",")
		return query.AuditKey{UserID: atoi(userID), Action: action}, ok
	})
	rows, err := h.app.LookupAuditLogs(r.Context(), keys)
	writeRows(w, rows, err)
}

func writeRows(w http.ResponseWriter, rows []query.Row, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// csv splits a comma-separated query value, trimming each element; an empty value yields nil.
func csv(s string) []string {
	if s == "" {
		return nil
	}
	return lo.Map(strings.Split(s, ","), func(p string, _ int) string { return strings.TrimSpace(p) })
}
