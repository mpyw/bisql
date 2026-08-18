// Command serve is the sample API application. It opens an in-memory SQLite database (via the
// pure-Go modernc.org/sqlite driver), migrates it, and serves the bisql-backed HTTP API, wiring
// the layers together by hand.
//
//	go run ./cmd/serve
//	curl 'localhost:8080/users?active_only=true&sort=name'
//	curl 'localhost:8080/reports/activity?since=2025-06-01'
//	curl -XPOST localhost:8080/audit-logs/batch -d '[{"userId":1,"action":"login"}]'
//	curl 'localhost:8080/audit-logs/lookup?key=1,login&key=2,login'
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"

	_ "modernc.org/sqlite"

	"github.com/mpyw/bisql/example/src/app"
	infraquery "github.com/mpyw/bisql/example/src/app/infrastructure/query"
	infraschema "github.com/mpyw/bisql/example/src/app/infrastructure/schema"
	"github.com/mpyw/bisql/example/src/app/presentation"
	appschema "github.com/mpyw/bisql/example/src/app/schema"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// A :memory: database lives inside a single connection, so pin the pool to one connection to
	// keep the schema and seed visible to every request.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	// Manual dependency injection: each adapter is built from the concrete infrastructure and
	// consumed through its port. The migrator brings the schema up; the query adapter backs the
	// application core, which the HTTP handler wraps.
	var migrator appschema.Migrator = infraschema.New(db)
	if err := migrator.Migrate(context.Background()); err != nil {
		log.Fatal(err)
	}

	q := infraquery.New(db)
	h := presentation.New(app.New(q, q, q, q))

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, h.Routes()))
}
