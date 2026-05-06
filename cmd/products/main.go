package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/config"
	pb "github.com/AndriyKalashnykov/dapr-go-hero/proto/products"
)

// server implements the Products gRPC service with an in-memory store.
type server struct {
	pb.UnimplementedProductsServer
	sync.RWMutex
	products map[string]*pb.Product
}

func newServer() *server {
	return &server{
		products: make(map[string]*pb.Product),
	}
}

func (s *server) GetProduct(ctx context.Context, in *pb.ProductRequest) (*pb.Product, error) {
	log.Println("GetProduct called", in.Id)
	s.RLock()
	defer s.RUnlock()

	product, ok := s.products[in.Id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "product %q not found", in.Id)
	}

	return product, nil
}

func (s *server) SaveProduct(ctx context.Context, product *pb.Product) (*emptypb.Empty, error) {
	log.Println("SaveProduct called", product)
	s.Lock()
	defer s.Unlock()

	s.products[product.Id] = product

	return &emptypb.Empty{}, nil
}

// runServer is the testable entrypoint extracted from main per
// /test-coverage-analysis Pattern C. It accepts an explicit listen address
// (so tests can pass `127.0.0.1:0` and pick a free port without flag
// parsing) and a context used to drive shutdown — when ctx is canceled,
// the gRPC server is gracefully stopped.
func runServer(ctx context.Context, addr string) error {
	lc := &net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", addr) // #nosec G102 -- caller chooses bind address; tests use 127.0.0.1:0
	if err != nil {
		return err
	}
	s := grpc.NewServer()
	pb.RegisterProductsServer(s, newServer())
	log.Printf("server listening at %v", lis.Addr())

	// Shutdown when ctx is canceled.
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(lis) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		s.GracefulStop()
		// Drain Serve's eventual return value.
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	// Explicit cleanup before os.Exit (gocritic exitAfterDefer).
	exitCode := 0
	if err := runServer(ctx, config.ProductsAddr); err != nil {
		log.Printf("runServer: %v", err)
		exitCode = 1
	}
	cancel()
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
