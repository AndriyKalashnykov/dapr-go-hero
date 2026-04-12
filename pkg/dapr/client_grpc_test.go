package dapr

import (
	"context"
	"testing"

	v1 "github.com/dapr/dapr/pkg/proto/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestEtagGRPC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  *v1.Etag
	}{
		{
			name:  "empty returns nil",
			value: "",
			want:  nil,
		},
		{
			name:  "non-empty returns Etag",
			value: "abc123",
			want:  &v1.Etag{Value: "abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := etagGRPC(tt.value)
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want non-nil")
			}
			if got.Value != tt.want.Value {
				t.Errorf("Value = %q, want %q", got.Value, tt.want.Value)
			}
		})
	}
}

func TestUnaryClientInterceptor(t *testing.T) {
	t.Parallel()

	t.Run("propagates incoming metadata", func(t *testing.T) {
		t.Parallel()

		inMD := metadata.Pairs("traceparent", "00-abc-def-01")
		ctx := metadata.NewIncomingContext(context.Background(), inMD)

		var capturedCtx context.Context
		invoker := grpc.UnaryInvoker(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			capturedCtx = ctx
			return nil
		})

		err := UnaryClientInterceptor(ctx, "/test.Method", nil, nil, nil, invoker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		md, ok := metadata.FromOutgoingContext(capturedCtx)
		if !ok {
			t.Fatal("no outgoing metadata propagated")
		}
		vals := md.Get("traceparent")
		if len(vals) != 1 || vals[0] != "00-abc-def-01" {
			t.Errorf("traceparent = %v, want [00-abc-def-01]", vals)
		}
	})

	t.Run("works without incoming metadata", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		called := false
		invoker := grpc.UnaryInvoker(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			called = true
			return nil
		})

		err := UnaryClientInterceptor(ctx, "/test.Method", nil, nil, nil, invoker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("invoker was not called")
		}
	})
}

func TestInvokingContext(t *testing.T) {
	t.Parallel()

	t.Run("adds dapr-app-id to empty context", func(t *testing.T) {
		t.Parallel()

		ctx := InvokingContext(context.Background(), "my-service")

		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Fatal("no outgoing metadata")
		}
		vals := md.Get("dapr-app-id")
		if len(vals) != 1 || vals[0] != "my-service" {
			t.Errorf("dapr-app-id = %v, want [my-service]", vals)
		}
	})

	t.Run("preserves existing incoming metadata", func(t *testing.T) {
		t.Parallel()

		inMD := metadata.Pairs("existing-key", "existing-value")
		ctx := metadata.NewIncomingContext(context.Background(), inMD)

		ctx = InvokingContext(ctx, "products")

		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Fatal("no outgoing metadata")
		}
		if vals := md.Get("existing-key"); len(vals) != 1 || vals[0] != "existing-value" {
			t.Errorf("existing-key = %v", vals)
		}
		if vals := md.Get("dapr-app-id"); len(vals) != 1 || vals[0] != "products" {
			t.Errorf("dapr-app-id = %v", vals)
		}
	})

	t.Run("does not mutate original incoming metadata", func(t *testing.T) {
		t.Parallel()

		inMD := metadata.Pairs("key", "value")
		ctx := metadata.NewIncomingContext(context.Background(), inMD)

		_ = InvokingContext(ctx, "service-a")

		// Original incoming metadata should not contain dapr-app-id
		originalMD, _ := metadata.FromIncomingContext(ctx)
		if vals := originalMD.Get("dapr-app-id"); len(vals) != 0 {
			t.Errorf("original metadata was mutated: dapr-app-id = %v", vals)
		}
	})
}
