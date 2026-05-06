package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/AndriyKalashnykov/dapr-go-hero/proto/products"
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	s := newServer()
	if s == nil {
		t.Fatal("newServer returned nil")
	}
	if s.products == nil {
		t.Error("products map not initialized")
	}
}

func TestSaveProduct(t *testing.T) {
	t.Parallel()

	s := newServer()
	ctx := context.Background()

	product := &pb.Product{
		Id:          "prod-1",
		Description: "Test Product",
		Price:       9.99,
	}

	resp, err := s.SaveProduct(ctx, product)
	if err != nil {
		t.Fatalf("SaveProduct error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify stored
	s.RLock()
	stored, ok := s.products["prod-1"]
	s.RUnlock()
	if !ok {
		t.Fatal("product not stored")
	}
	if stored.Description != "Test Product" {
		t.Errorf("Description = %q, want %q", stored.Description, "Test Product")
	}
	if stored.Price != 9.99 {
		t.Errorf("Price = %f, want 9.99", stored.Price)
	}
}

func TestSaveProduct_Overwrites(t *testing.T) {
	t.Parallel()

	s := newServer()
	ctx := context.Background()

	_, _ = s.SaveProduct(ctx, &pb.Product{Id: "prod-1", Description: "v1", Price: 1.0})
	_, _ = s.SaveProduct(ctx, &pb.Product{Id: "prod-1", Description: "v2", Price: 2.0})

	s.RLock()
	stored := s.products["prod-1"]
	s.RUnlock()

	if stored.Description != "v2" {
		t.Errorf("Description = %q, want %q", stored.Description, "v2")
	}
	if stored.Price != 2.0 {
		t.Errorf("Price = %f, want 2.0", stored.Price)
	}
}

func TestGetProduct_Exists(t *testing.T) {
	t.Parallel()

	s := newServer()
	ctx := context.Background()

	_, _ = s.SaveProduct(ctx, &pb.Product{Id: "prod-1", Description: "Widget", Price: 5.0})

	product, err := s.GetProduct(ctx, &pb.ProductRequest{Id: "prod-1"})
	if err != nil {
		t.Fatalf("GetProduct error: %v", err)
	}
	if product.Id != "prod-1" {
		t.Errorf("Id = %q, want %q", product.Id, "prod-1")
	}
	if product.Description != "Widget" {
		t.Errorf("Description = %q, want %q", product.Description, "Widget")
	}
	if product.Price != 5.0 {
		t.Errorf("Price = %f, want 5.0", product.Price)
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	t.Parallel()

	s := newServer()

	_, err := s.GetProduct(context.Background(), &pb.ProductRequest{Id: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing product")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("code = %v, want NotFound", st.Code())
	}
}

// TestRunServer_ServesAndShutsDown verifies the extracted runServer
// entrypoint binds an ephemeral port (avoiding any 50151 collision with
// a real Dapr setup or sibling test runs), serves gRPC requests, and
// shuts down cleanly when ctx is canceled. Uses 127.0.0.1:0 so the
// kernel picks a free port — no hardcoded value.
func TestRunServer_ServesAndShutsDown(t *testing.T) {
	t.Parallel()

	// Bind first to discover the free port, then close so runServer can
	// re-bind. (We can't pass the listener into runServer without
	// changing its signature; the bind+close+rebind pattern is the
	// canonical workaround for "give me an ephemeral port the callee
	// will then bind.")
	lc := &net.ListenConfig{}
	probe, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serveDone := make(chan error, 1)
	go func() { serveDone <- runServer(ctx, addr) }()

	// Wait for the server to start accepting; gRPC dial with WithBlock
	// would loop forever on its own, so cap the wait.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	var conn *grpc.ClientConn
	for {
		if dialCtx.Err() != nil {
			t.Fatalf("dial timeout: %v", dialCtx.Err())
		}
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Round-trip a SaveProduct + GetProduct to prove the wiring is real.
	client := pb.NewProductsClient(conn)
	if _, err := client.SaveProduct(context.Background(), &pb.Product{
		Id: "p-1", Description: "test", Price: 1.23,
	}); err != nil {
		t.Fatalf("SaveProduct: %v", err)
	}
	got, err := client.GetProduct(context.Background(), &pb.ProductRequest{Id: "p-1"})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if got.Description != "test" || got.Price != 1.23 {
		t.Errorf("got %+v", got)
	}

	// Cancel ctx and verify runServer returns nil within a bounded time.
	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("runServer returned error on cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer did not shut down within 5s after ctx cancel")
	}
}

func TestRunServer_FailsOnBadAddr(t *testing.T) {
	t.Parallel()

	// "x" is not a valid TCP address — Listen rejects it before any goroutine starts.
	err := runServer(context.Background(), "not-a-valid-addr")
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := newServer()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "prod-" + string(rune('A'+n%26))
			_, _ = s.SaveProduct(ctx, &pb.Product{Id: id, Description: "concurrent", Price: float64(n)})
			_, _ = s.GetProduct(ctx, &pb.ProductRequest{Id: id})
		}(i)
	}
	wg.Wait()
}
