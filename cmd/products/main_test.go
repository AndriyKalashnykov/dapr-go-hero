package main

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
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
