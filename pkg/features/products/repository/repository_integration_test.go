//go:build integration

// Integration test for the products repository against a real gRPC Products
// server. A minimal in-memory implementation (identical semantics to
// cmd/products/main.go, which is a main package and cannot be imported) is
// started on an ephemeral TCP port; the repository dials it like it would
// the Dapr sidecar.
package repository_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products/repository"
	pb "github.com/AndriyKalashnykov/dapr-go-hero/proto/products"
)

type productsServer struct {
	pb.UnimplementedProductsServer
	mu    sync.RWMutex
	items map[string]*pb.Product
}

func (s *productsServer) GetProduct(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.items[in.Id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "product %q not found", in.Id)
	}
	return p, nil
}

func (s *productsServer) SaveProduct(ctx context.Context, in *pb.Product) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[in.Id] = in
	return &emptypb.Empty{}, nil
}

// startProducts starts a Products gRPC server on an ephemeral port, and
// rewires the repository's package-level GRPCADDRESS so New() dials it.
// Returns the address for assertions.
func startProducts(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	gs := grpc.NewServer()
	pb.RegisterProductsServer(gs, &productsServer{items: map[string]*pb.Product{}})
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(gs.GracefulStop)

	addr := ln.Addr().String()
	orig := repository.GRPCADDRESS
	repository.GRPCADDRESS = addr
	t.Cleanup(func() { repository.GRPCADDRESS = orig })
	return addr
}

func newRepo(t *testing.T) *repository.Repository {
	t.Helper()
	startProducts(t)
	repo, err := repository.New(logr.Discard())
	if err != nil {
		t.Fatalf("repository.New: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestRepository_Save_RoundTripsViaGRPC(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	want := &products.Product{ID: "p1", Description: "Widget grinder", Price: 12.99}
	if err := repo.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Load(ctx, "p1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != want.ID || got.Description != want.Description || got.Price != want.Price {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestRepository_Load_ReturnsNotFoundFromGRPCStatus(t *testing.T) {
	repo := newRepo(t)

	_, err := repo.Load(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing product")
	}
	var ez *errorz.Error
	if !errors.As(err, &ez) {
		t.Fatalf("expected *errorz.Error, got %T: %v", err, err)
	}
	if ez.Code != 404 {
		t.Errorf("Code = %d, want 404", ez.Code)
	}
}

func TestRepository_Save_OverwritesExisting(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	if err := repo.Save(ctx, &products.Product{ID: "p2", Description: "v1", Price: 1}); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	if err := repo.Save(ctx, &products.Product{ID: "p2", Description: "v2", Price: 2}); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	got, err := repo.Load(ctx, "p2")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Description != "v2" || got.Price != 2 {
		t.Errorf("got %+v, want overwritten v2/2", got)
	}
}
