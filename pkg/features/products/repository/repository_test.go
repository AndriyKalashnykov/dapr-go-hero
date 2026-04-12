package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products"
	pb "github.com/AndriyKalashnykov/dapr-go-hero/proto/products"
)

// mockProductsClient implements pb.ProductsClient for testing.
type mockProductsClient struct {
	pb.ProductsClient
	saveFunc func(ctx context.Context, in *pb.Product) (*emptypb.Empty, error)
	getFunc  func(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error)
}

func (m *mockProductsClient) SaveProduct(ctx context.Context, in *pb.Product, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	return m.saveFunc(ctx, in)
}

func (m *mockProductsClient) GetProduct(ctx context.Context, in *pb.ProductRequest, opts ...grpc.CallOption) (*pb.Product, error) {
	return m.getFunc(ctx, in)
}

func newTestRepo(client pb.ProductsClient) *Repository {
	return &Repository{
		log:    logr.Discard(),
		client: client,
	}
}

func TestSave_Success(t *testing.T) {
	t.Parallel()

	var capturedProduct *pb.Product
	client := &mockProductsClient{
		saveFunc: func(ctx context.Context, in *pb.Product) (*emptypb.Empty, error) {
			capturedProduct = in
			return &emptypb.Empty{}, nil
		},
	}

	repo := newTestRepo(client)
	err := repo.Save(context.Background(), &products.Product{
		ID:          "p1",
		Description: "Test Product",
		Price:       42.0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedProduct == nil {
		t.Fatal("SaveProduct was not called")
	}
	if capturedProduct.Id != "p1" {
		t.Errorf("Id = %q, want %q", capturedProduct.Id, "p1")
	}
	if capturedProduct.Description != "Test Product" {
		t.Errorf("Description = %q", capturedProduct.Description)
	}
	if capturedProduct.Price != 42.0 {
		t.Errorf("Price = %f, want 42.0", capturedProduct.Price)
	}
}

func TestSave_Error(t *testing.T) {
	t.Parallel()

	client := &mockProductsClient{
		saveFunc: func(ctx context.Context, in *pb.Product) (*emptypb.Empty, error) {
			return nil, status.Error(codes.Unavailable, "service down")
		},
	}

	repo := newTestRepo(client)
	err := repo.Save(context.Background(), &products.Product{ID: "p1"})

	if err == nil {
		t.Fatal("expected error from SaveProduct")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Parallel()

	client := &mockProductsClient{
		getFunc: func(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error) {
			if in.Id != "p1" {
				t.Errorf("Id = %q, want %q", in.Id, "p1")
			}
			return &pb.Product{
				Id:          "p1",
				Description: "Loaded Product",
				Price:       55.5,
			}, nil
		},
	}

	repo := newTestRepo(client)
	product, err := repo.Load(context.Background(), "p1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if product.ID != "p1" {
		t.Errorf("ID = %q, want %q", product.ID, "p1")
	}
	if product.Description != "Loaded Product" {
		t.Errorf("Description = %q", product.Description)
	}
	if product.Price != 55.5 {
		t.Errorf("Price = %f, want 55.5", product.Price)
	}
}

func TestLoad_NotFound(t *testing.T) {
	t.Parallel()

	client := &mockProductsClient{
		getFunc: func(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error) {
			return nil, status.Errorf(codes.NotFound, "product %q not found", in.Id)
		},
	}

	repo := newTestRepo(client)
	_, err := repo.Load(context.Background(), "nonexistent")

	if err == nil {
		t.Fatal("expected error for missing product")
	}
	var errz *errorz.Error
	if !errors.As(err, &errz) {
		t.Fatalf("expected *errorz.Error, got %T: %v", err, err)
	}
	if errz.Code != 404 {
		t.Errorf("Code = %d, want 404", errz.Code)
	}
}

func TestLoad_OtherError(t *testing.T) {
	t.Parallel()

	client := &mockProductsClient{
		getFunc: func(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error) {
			return nil, status.Error(codes.Internal, "database error")
		},
	}

	repo := newTestRepo(client)
	_, err := repo.Load(context.Background(), "p1")

	if err == nil {
		t.Fatal("expected error from GetProduct")
	}
}
