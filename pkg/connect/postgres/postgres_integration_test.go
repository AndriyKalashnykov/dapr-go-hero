//go:build integration

// Integration test for postgres.Connect — verifies that the secret-fetch +
// pool-construction + Ping + AfterConnect chain works end-to-end against a
// real PostgreSQL container via testcontainers-go.
package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/connect/postgres"
)

// fakeSecretsStore satisfies secrets.Store. It encodes a struct as JSON and
// unmarshals into the caller's target — matching the wire shape Dapr's
// secret store uses (the real Dapr GetSecret returns a map[string]string
// that the client_http / client_grpc implementations marshal+unmarshal
// into the caller's typed target).
type fakeSecretsStore struct {
	data        any
	getSecretFn func(ctx context.Context, store, name string, target any) error // override hook for error tests
}

func (f *fakeSecretsStore) GetSecret(ctx context.Context, store, name string, target any) error {
	if f.getSecretFn != nil {
		return f.getSecretFn(ctx, store, name, target)
	}
	b, err := json.Marshal(f.data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

// runPostgres starts a disposable PostgreSQL container and returns the
// host:port and the test database name so the caller can synthesize
// DBCreds for the fake secret store.
func runPostgres(t *testing.T) (host string, port string, user, pass, db string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pg, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })

	host, err = pg.Host(ctx)
	if err != nil {
		t.Fatalf("Host: %v", err)
	}
	mp, err := pg.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("MappedPort: %v", err)
	}
	return host, mp.Port(), "postgres", "postgres", "testdb"
}

func TestConnect_HappyPath(t *testing.T) {
	host, port, user, pass, db := runPostgres(t)

	store := &fakeSecretsStore{data: postgres.DBCreds{
		Host:     fmt.Sprintf("%s:%s", host, port),
		Username: user,
		Password: pass,
		Database: db,
	}}

	pool, err := postgres.Connect(context.Background(), store, "secrets", "postgres")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Ping passed implicitly inside Connect; do an additional round-trip to
	// confirm the returned pool is actually usable.
	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if one != 1 {
		t.Errorf("got %d, want 1", one)
	}
}

func TestConnect_AfterConnectFires(t *testing.T) {
	host, port, user, pass, db := runPostgres(t)

	store := &fakeSecretsStore{data: postgres.DBCreds{
		Host:     fmt.Sprintf("%s:%s", host, port),
		Username: user,
		Password: pass,
		Database: db,
	}}

	var afterCalls int
	afterConnect := func(ctx context.Context, conn *pgx.Conn) error {
		afterCalls++
		// Real callers use this to create a schema or register prepared
		// statements; emulate by creating a marker table.
		_, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS connect_marker (k INT)")
		return err
	}

	pool, err := postgres.Connect(context.Background(), store, "secrets", "postgres", afterConnect)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if afterCalls < 1 {
		t.Errorf("AfterConnect was not invoked (call count = %d)", afterCalls)
	}
	// Also verify the side effect actually landed in the database.
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM information_schema.tables WHERE table_name = 'connect_marker'").Scan(&n); err != nil {
		t.Fatalf("verify table: %v", err)
	}
	if n != 1 {
		t.Errorf("connect_marker table count = %d, want 1", n)
	}
}

func TestConnect_PropagatesSecretFetchError(t *testing.T) {
	store := &fakeSecretsStore{
		getSecretFn: func(ctx context.Context, store, name string, target any) error {
			return fmt.Errorf("secret store unreachable")
		},
	}
	_, err := postgres.Connect(context.Background(), store, "secrets", "postgres")
	if err == nil {
		t.Fatal("expected error from secret-store failure")
	}
	if got := err.Error(); got != "secret store unreachable" {
		t.Errorf("error = %q, want propagated secret-store error", got)
	}
}

func TestConnect_FailsOnUnreachableHost(t *testing.T) {
	// Synthesize creds pointing at a port nothing is listening on. The
	// pgxpool.NewWithConfig call may succeed (lazy connect) but Ping will
	// fail before the function returns.
	store := &fakeSecretsStore{data: postgres.DBCreds{
		Host:     "127.0.0.1:1", // reserved-low port, refused
		Username: "x",
		Password: "x",
		Database: "x",
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := postgres.Connect(ctx, store, "secrets", "postgres")
	if err == nil {
		pool.Close()
		t.Fatal("expected error connecting to refused port")
	}
}
