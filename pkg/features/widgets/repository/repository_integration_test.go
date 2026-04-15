//go:build integration

// Package repository integration tests validate the widget repository
// against a real PostgreSQL instance started via testcontainers-go.
// Run: make integration-test
package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/widgets"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/widgets/repository"
)

const initSQL = `
CREATE TABLE IF NOT EXISTS widgets (
  id varchar(256) PRIMARY KEY,
  description varchar(4000) NOT NULL,
  price float NOT NULL
);
`

// setupPostgres starts a disposable PostgreSQL container and returns a pool
// connected to it. The prepared statements required by the repository
// (upsert, select) are registered via AfterConnect.
func setupPostgres(t *testing.T) *pgxpool.Pool {
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
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Terminate(context.Background())
	})

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	// Step 1: create the schema using a bare pool (no AfterConnect — it
	// would fail Prepare() because the table doesn't exist yet).
	bareCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	bare, err := pgxpool.NewWithConfig(ctx, bareCfg)
	if err != nil {
		t.Fatalf("bare NewWithConfig: %v", err)
	}
	if _, err := bare.Exec(ctx, initSQL); err != nil {
		bare.Close()
		t.Fatalf("init SQL: %v", err)
	}
	bare.Close()

	// Step 2: open the real pool with AfterConnect — prepared statements
	// now succeed because the table exists.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.AfterConnect = repository.AfterConnect

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRepository_Save_InsertsNewWidget(t *testing.T) {
	pool := setupPostgres(t)
	repo := repository.New(logr.Discard(), pool)
	ctx := context.Background()

	w := &widgets.Widget{ID: "w1", Description: "First Widget", Price: 9.99}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Load(ctx, "w1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "w1" || got.Description != "First Widget" || got.Price != 9.99 {
		t.Errorf("got %+v, want {w1 First Widget 9.99}", got)
	}
}

func TestRepository_Save_UpsertsExistingWidget(t *testing.T) {
	pool := setupPostgres(t)
	repo := repository.New(logr.Discard(), pool)
	ctx := context.Background()

	if err := repo.Save(ctx, &widgets.Widget{ID: "w2", Description: "v1", Price: 1.0}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := repo.Save(ctx, &widgets.Widget{ID: "w2", Description: "v2", Price: 2.0}); err != nil {
		t.Fatalf("upsert Save: %v", err)
	}

	got, err := repo.Load(ctx, "w2")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Description != "v2" || got.Price != 2.0 {
		t.Errorf("got %+v, want upserted v2/2.0", got)
	}
}

func TestRepository_Load_ReturnsNotFoundForMissingWidget(t *testing.T) {
	pool := setupPostgres(t)
	repo := repository.New(logr.Discard(), pool)

	_, err := repo.Load(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing widget")
	}
	var errz *errorz.Error
	if !errors.As(err, &errz) {
		t.Fatalf("expected *errorz.Error, got %T: %v", err, err)
	}
	if errz.Code != 404 {
		t.Errorf("Code = %d, want 404", errz.Code)
	}
}

// TestRepository_ConcurrentSaveLoad exercises the pgx pool under contention.
// Each goroutine inserts and reads its own key, so results are deterministic
// — any data race, connection leak, or AfterConnect regression surfaces here.
func TestRepository_ConcurrentSaveLoad(t *testing.T) {
	pool := setupPostgres(t)
	repo := repository.New(logr.Discard(), pool)
	ctx := context.Background()

	const workers = 16
	const iters = 5

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iters)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				id := fmt.Sprintf("c-%d-%d", worker, i)
				want := &widgets.Widget{ID: id, Description: id + "-desc", Price: float64(i)}
				if err := repo.Save(ctx, want); err != nil {
					errCh <- fmt.Errorf("save %s: %w", id, err)
					return
				}
				got, err := repo.Load(ctx, id)
				if err != nil {
					errCh <- fmt.Errorf("load %s: %w", id, err)
					return
				}
				if got.Description != want.Description || got.Price != want.Price {
					errCh <- fmt.Errorf("mismatch %s: got %+v", id, got)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestRepository_Save_PreventsSQLInjection(t *testing.T) {
	pool := setupPostgres(t)
	repo := repository.New(logr.Discard(), pool)
	ctx := context.Background()

	// Attempt SQL injection via the ID field. Parameterized queries should
	// store this as a literal ID, not execute it as SQL.
	evil := &widgets.Widget{
		ID:          "w3'; DROP TABLE widgets; --",
		Description: "Inject Attempt",
		Price:       0,
	}
	if err := repo.Save(ctx, evil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Table must still exist — lookup by the literal evil ID should return it.
	got, err := repo.Load(ctx, evil.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != evil.ID {
		t.Errorf("ID not preserved: got %q, want %q", got.ID, evil.ID)
	}
}
