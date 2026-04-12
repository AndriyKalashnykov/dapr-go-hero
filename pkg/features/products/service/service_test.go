package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/go-sdk/service/common"
	"github.com/go-logr/logr"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products"
)

type mockStore struct {
	saveFunc func(ctx context.Context, product *products.Product) error
	loadFunc func(ctx context.Context, id string) (*products.Product, error)
}

func (m *mockStore) Save(ctx context.Context, product *products.Product) error {
	return m.saveFunc(ctx, product)
}

func (m *mockStore) Load(ctx context.Context, id string) (*products.Product, error) {
	return m.loadFunc(ctx, id)
}

func TestNew(t *testing.T) {
	t.Parallel()

	svc := New(logr.Discard(), &mockStore{})
	if svc == nil {
		t.Fatal("New returned nil")
	}
}

func TestSubscriptions(t *testing.T) {
	t.Parallel()

	svc := New(logr.Discard(), &mockStore{})
	subs := svc.Subscriptions()

	if len(subs) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(subs))
	}
	if subs[0].PubsubName != "pubsub" {
		t.Errorf("PubsubName = %q", subs[0].PubsubName)
	}
	if subs[0].Topic != "inventory" {
		t.Errorf("Topic = %q", subs[0].Topic)
	}
	// Products use default route, no rules
	if len(subs[0].Routes.Rules) != 0 {
		t.Errorf("got %d rules, want 0 (products uses default route)", len(subs[0].Routes.Rules))
	}
	if subs[0].Routes.Default != "/products.v1" {
		t.Errorf("Default = %q, want %q", subs[0].Routes.Default, "/products.v1")
	}
}

func TestRegisterTopicEventHandlers(t *testing.T) {
	t.Parallel()

	svc := New(logr.Discard(), &mockStore{})

	var registeredPath string
	svc.RegisterTopicEventHandlers(func(path string, handler dapr.TopicEventHandler) {
		registeredPath = path
	})

	if registeredPath != "/products.v1" {
		t.Errorf("registered path = %q, want %q", registeredPath, "/products.v1")
	}
}

func TestSaveGRPC_Success(t *testing.T) {
	t.Parallel()

	var saved *products.Product
	store := &mockStore{
		saveFunc: func(ctx context.Context, product *products.Product) error {
			saved = product
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(products.Product{ID: "p1", Description: "Thingamajig", Price: 49.99})
	resp, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: data})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != pb.TopicEventResponse_SUCCESS {
		t.Errorf("Status = %v, want SUCCESS", resp.Status)
	}
	if saved == nil {
		t.Fatal("store.Save was not called")
	}
	if saved.ID != "p1" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "p1")
	}
	if saved.Description != "Thingamajig" {
		t.Errorf("saved.Description = %q", saved.Description)
	}
}

func TestSaveGRPC_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, product *products.Product) error {
			t.Error("save should not be called")
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: []byte(`{bad}`)})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveGRPC_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, product *products.Product) error {
			return errors.New("gRPC service unavailable")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(products.Product{ID: "p1"})
	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: data})

	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestSaveSDK_Success(t *testing.T) {
	t.Parallel()

	var saved *products.Product
	store := &mockStore{
		saveFunc: func(ctx context.Context, product *products.Product) error {
			saved = product
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(products.Product{ID: "p2", Description: "SDK Product", Price: 99.0})
	event := &common.TopicEvent{RawData: data}

	retry, err := svc.SaveSDK(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retry {
		t.Error("retry should be false")
	}
	if saved == nil {
		t.Fatal("store.Save was not called")
	}
	if saved.ID != "p2" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "p2")
	}
}

func TestSaveSDK_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, product *products.Product) error {
			return errors.New("remote error")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(products.Product{ID: "p3"})
	event := &common.TopicEvent{RawData: data}

	retry, err := svc.SaveSDK(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from store")
	}
	if retry {
		t.Error("retry should be false")
	}
}
