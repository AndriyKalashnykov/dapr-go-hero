//go:build integration

// Integration test for the custom gRPC Dapr client. A stub gRPC server
// impersonates the Dapr sidecar gRPC API (SaveState / GetState / GetSecret)
// so we can verify the client's request shape, store-name routing, ETag
// propagation, and JSON encoding of state values without pulling in the
// real sidecar.
//
// This is the gRPC counterpart to client_http_integration_test.go.
package dapr_test

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"

	v1 "github.com/dapr/dapr/pkg/proto/common/v1"
	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
)

// stubGRPCSidecar implements just enough of the Dapr DaprServer surface for
// the three methods the GRPC client invokes. Embeds UnimplementedDaprServer
// so any other RPC the client adds in the future returns Unimplemented
// rather than a nil-deref.
type stubGRPCSidecar struct {
	pb.UnimplementedDaprServer
	mu sync.Mutex

	lastSaveStateReq   *pb.SaveStateRequest
	lastGetStateReq    *pb.GetStateRequest
	lastGetSecretReq   *pb.GetSecretRequest
	getStateResponse   *pb.GetStateResponse
	getSecretResponse  *pb.GetSecretResponse
}

func (s *stubGRPCSidecar) SaveState(ctx context.Context, req *pb.SaveStateRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSaveStateReq = req
	return &emptypb.Empty{}, nil
}

func (s *stubGRPCSidecar) GetState(ctx context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastGetStateReq = req
	if s.getStateResponse != nil {
		return s.getStateResponse, nil
	}
	return &pb.GetStateResponse{}, nil
}

func (s *stubGRPCSidecar) GetSecret(ctx context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastGetSecretReq = req
	if s.getSecretResponse != nil {
		return s.getSecretResponse, nil
	}
	return &pb.GetSecretResponse{}, nil
}

func startGRPCStub(t *testing.T, s *stubGRPCSidecar) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterDaprServer(srv, s)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Stop() })

	orig := dapr.GRPCADDRESS
	dapr.GRPCADDRESS = ln.Addr().String()
	t.Cleanup(func() { dapr.GRPCADDRESS = orig })
}

func newGRPCClient(t *testing.T, s *stubGRPCSidecar) *dapr.GRPC {
	t.Helper()
	startGRPCStub(t, s)
	c, err := dapr.NewGRPC(context.Background())
	if err != nil {
		t.Fatalf("NewGRPC: %v", err)
	}
	return c
}

func TestGRPC_SetState_PassesStoreNameAndJSONEncodesValue(t *testing.T) {
	stub := &stubGRPCSidecar{}
	c := newGRPCClient(t, stub)

	payload := map[string]any{"id": "w-1", "price": 9.99}
	err := c.SetState(context.Background(), "statestore",
		state.Item{Key: "widget:w-1", Value: payload})
	if err != nil {
		t.Fatalf("SetState: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastSaveStateReq == nil {
		t.Fatal("SaveState was not called")
	}
	if stub.lastSaveStateReq.StoreName != "statestore" {
		t.Errorf("StoreName = %q, want statestore", stub.lastSaveStateReq.StoreName)
	}
	if got := len(stub.lastSaveStateReq.States); got != 1 {
		t.Fatalf("len(States) = %d, want 1", got)
	}
	si := stub.lastSaveStateReq.States[0]
	if si.Key != "widget:w-1" {
		t.Errorf("Key = %q", si.Key)
	}
	// Value is JSON-encoded by the client; round-trip to verify shape.
	var roundTrip map[string]any
	if err := json.Unmarshal(si.Value, &roundTrip); err != nil {
		t.Fatalf("Value not valid JSON: %v\n%s", err, si.Value)
	}
	if roundTrip["id"] != "w-1" {
		t.Errorf("Value[id] = %v, want w-1", roundTrip["id"])
	}
}

func TestGRPC_SetState_OmitsETagWhenEmpty(t *testing.T) {
	stub := &stubGRPCSidecar{}
	c := newGRPCClient(t, stub)

	err := c.SetState(context.Background(), "statestore",
		state.Item{Key: "k", Value: "v"})
	if err != nil {
		t.Fatalf("SetState: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	si := stub.lastSaveStateReq.States[0]
	// Empty etag must produce a nil Etag (not Etag{Value:""}) — Redis state
	// component treats Etag{Value:""} as a CAS-against-empty assertion.
	if si.Etag != nil {
		t.Errorf("Etag = %v, want nil for empty input", si.Etag)
	}
}

func TestGRPC_SetState_PassesNonEmptyETagAsValue(t *testing.T) {
	stub := &stubGRPCSidecar{}
	c := newGRPCClient(t, stub)

	err := c.SetState(context.Background(), "statestore",
		state.Item{Key: "k", Value: "v", ETag: "v42"})
	if err != nil {
		t.Fatalf("SetState: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	si := stub.lastSaveStateReq.States[0]
	if si.Etag == nil || si.Etag.Value != "v42" {
		t.Errorf("Etag = %v, want v42", si.Etag)
	}
}

func TestGRPC_GetState_DecodesJSONIntoTarget(t *testing.T) {
	stub := &stubGRPCSidecar{
		getStateResponse: &pb.GetStateResponse{
			Data: []byte(`{"id":"w-1","price":9.99}`),
		},
	}
	c := newGRPCClient(t, stub)

	var got struct {
		ID    string  `json:"id"`
		Price float64 `json:"price"`
	}
	if err := c.GetState(context.Background(), "statestore", "widget:w-1", &got); err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got.ID != "w-1" || got.Price != 9.99 {
		t.Errorf("decoded = %+v", got)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastGetStateReq.Key != "widget:w-1" {
		t.Errorf("Key = %q", stub.lastGetStateReq.Key)
	}
	if stub.lastGetStateReq.Consistency != v1.StateOptions_CONSISTENCY_STRONG {
		t.Errorf("Consistency = %v, want STRONG", stub.lastGetStateReq.Consistency)
	}
}

func TestGRPC_GetState_ReturnsNotFoundWhenDataNil(t *testing.T) {
	stub := &stubGRPCSidecar{
		getStateResponse: &pb.GetStateResponse{Data: nil},
	}
	c := newGRPCClient(t, stub)

	var v any
	err := c.GetState(context.Background(), "statestore", "missing", &v)
	if err == nil {
		t.Fatal("expected NotFound error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, "missing") {
		t.Errorf("error message = %q, want it to mention key", msg)
	}
}

func TestGRPC_GetSecret_DecodesMapIntoTarget(t *testing.T) {
	stub := &stubGRPCSidecar{
		getSecretResponse: &pb.GetSecretResponse{
			Data: map[string]string{"password": "s3cret", "user": "postgres"},
		},
	}
	c := newGRPCClient(t, stub)

	var got map[string]string
	if err := c.GetSecret(context.Background(), "secrets", "postgres", &got); err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got["password"] != "s3cret" {
		t.Errorf("password = %q", got["password"])
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastGetSecretReq.StoreName != "secrets" || stub.lastGetSecretReq.Key != "postgres" {
		t.Errorf("GetSecretRequest = %+v", stub.lastGetSecretReq)
	}
}

func TestGRPC_GetSecret_ReturnsNotFoundWhenDataNil(t *testing.T) {
	stub := &stubGRPCSidecar{
		getSecretResponse: &pb.GetSecretResponse{Data: nil},
	}
	c := newGRPCClient(t, stub)

	var got map[string]string
	err := c.GetSecret(context.Background(), "secrets", "missing", &got)
	if err == nil {
		t.Fatal("expected NotFound error, got nil")
	}
}

