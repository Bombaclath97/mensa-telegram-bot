// Package testdb starts throwaway PostgreSQL instances for tests.
//
// It exists so the `database` and `cleanup` test packages can share one setup without
// importing each other. It deliberately does NOT import the database package: it only hands
// back a DSN, leaving the caller to feed it to database.Init() via DATABASE_URL. That keeps
// it free of an import cycle and free of opinions about how the DSN is consumed.
package testdb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// image pins the Postgres version so tests exercise the same major version as production.
// A floating tag would let a silent upgrade change AutoMigrate's behaviour under us.
const image = "postgres:16-alpine"

const (
	dbName = "mensa_test"
	dbUser = "mensa"
	dbPass = "mensa"
)

// Start boots a throwaway Postgres and returns its DSN plus a terminate func.
//
// Tests run against the REAL engine, not a stand-in. That matters more than usual here: this
// schema leans on foreign keys with ON DELETE CASCADE, and several tests exist specifically
// to prove the cascades fire. A different engine — or SQLite, which the suite used to use and
// which disables foreign keys per-connection by default — would let a broken cascade pass.
//
// Returns an error rather than failing the test itself so TestMain can decide: a missing
// Docker daemon should skip, not fail the build.
func Start(ctx context.Context) (dsn string, terminate func(), err error) {
	container, err := postgres.Run(ctx, image,
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPass),
		// Postgres starts, runs its init scripts, then RESTARTS. Waiting for the log line
		// only once catches the first start and hands back a DSN that is about to stop
		// accepting connections — hence Occurrence(2), which is the documented remedy.
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return "", nil, fmt.Errorf("starting postgres container: %w", err)
	}

	terminate = func() {
		_ = testcontainers.TerminateContainer(container, testcontainers.StopTimeout(10*time.Second))
	}

	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		terminate()
		return "", nil, fmt.Errorf("building connection string: %w", err)
	}

	return dsn, terminate, nil
}

// MustStart is Start for use inside a test, skipping when Docker is unavailable.
func MustStart(t *testing.T) string {
	t.Helper()
	dsn, terminate, err := Start(context.Background())
	if err != nil {
		t.Skipf("skipping: could not start a postgres container (is Docker running?): %v", err)
	}
	t.Cleanup(terminate)
	return dsn
}
