package presentation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samber/lo"
	_ "modernc.org/sqlite"

	"github.com/mpyw/bisql/example/src/app"
	infraquery "github.com/mpyw/bisql/example/src/app/infrastructure/query"
	infraschema "github.com/mpyw/bisql/example/src/app/infrastructure/schema"
	"github.com/mpyw/bisql/example/src/app/presentation"
)

// newServer wires the whole stack — infrastructure adapters, application core, HTTP handler —
// over a freshly migrated in-memory SQLite database, exactly as cmd/serve does. Because each
// query runs for real, these tests double as proof that the bisql-built SQL is valid SQLite.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := infraschema.New(db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	q := infraquery.New(db)
	h := presentation.New(app.New(q, q, q, q))
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) []map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %d: %s", path, resp.StatusCode, b)
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func names(rows []map[string]any) []string {
	return lo.Map(rows, func(r map[string]any, _ int) string {
		name, _ := r["name"].(string)
		return name
	})
}

func TestSearchUsers_ActiveSortedByName(t *testing.T) {
	srv := newServer(t)
	rows := get(t, srv, "/users?active_only=true&sort=name")
	if got := names(rows); strings.Join(got, ",") != "Alice,Bob,Dave" {
		t.Errorf("names = %v, want [Alice Bob Dave]", got)
	}
}

func TestSearchUsers_TagAndDepartment(t *testing.T) {
	srv := newServer(t)
	rows := get(t, srv, "/users?tags=vip&with_department=true&sort=name")
	if got := names(rows); strings.Join(got, ",") != "Alice,Dave" {
		t.Fatalf("names = %v, want [Alice Dave]", got)
	}
	if dept, _ := rows[0]["department"].(string); dept != "Engineering" {
		t.Errorf("Alice department = %q, want Engineering", dept)
	}
}

func TestActivityReport_Windowed(t *testing.T) {
	srv := newServer(t)
	rows := get(t, srv, "/reports/activity?since=2025-06-01")
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want 2", rows)
	}
	if dept, _ := rows[0]["department"].(string); dept != "Sales" {
		t.Errorf("top department = %q, want Sales", dept)
	}
	if events, _ := rows[0]["events"].(float64); events != 2 {
		t.Errorf("Sales events = %v, want 2", rows[0]["events"])
	}
}

func TestBulkInsertThenLookup(t *testing.T) {
	srv := newServer(t)

	resp, err := http.Post(srv.URL+"/audit-logs/batch", "application/json",
		strings.NewReader(`[{"userId":1,"action":"view"},{"userId":2,"action":"view"}]`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var ins struct {
		Inserted int `json:"inserted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ins); err != nil {
		t.Fatal(err)
	}
	if ins.Inserted != 2 {
		t.Fatalf("inserted = %d, want 2", ins.Inserted)
	}

	rows := get(t, srv, "/audit-logs/lookup?key=1,view&key=2,view")
	if len(rows) != 2 {
		t.Errorf("lookup returned %d rows, want 2", len(rows))
	}
}
