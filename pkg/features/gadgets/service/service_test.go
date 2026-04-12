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
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets"
)

type mockStore struct {
	saveFunc func(ctx context.Context, gadget *gadgets.Gadget) error
	loadFunc func(ctx context.Context, id string) (*gadgets.Gadget, error)
}

func (m *mockStore) Save(ctx context.Context, gadget *gadgets.Gadget) error {
	return m.saveFunc(ctx, gadget)
}

func (m *mockStore) Load(ctx context.Context, id string) (*gadgets.Gadget, error) {
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
	if len(subs[0].Routes.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(subs[0].Routes.Rules))
	}
	if subs[0].Routes.Rules[0].Match != `event.type == "gadget.v1"` {
		t.Errorf("Match = %q", subs[0].Routes.Rules[0].Match)
	}
	if subs[0].Routes.Rules[0].Path != "/gadgets.v1" {
		t.Errorf("Path = %q", subs[0].Routes.Rules[0].Path)
	}
}

func TestRegisterTopicEventHandlers(t *testing.T) {
	t.Parallel()

	svc := New(logr.Discard(), &mockStore{})

	var registeredPath string
	svc.RegisterTopicEventHandlers(func(path string, handler dapr.TopicEventHandler) {
		registeredPath = path
	})

	if registeredPath != "/gadgets.v1" {
		t.Errorf("registered path = %q, want %q", registeredPath, "/gadgets.v1")
	}
}

func TestSaveGRPC_Success(t *testing.T) {
	t.Parallel()

	var saved *gadgets.Gadget
	store := &mockStore{
		saveFunc: func(ctx context.Context, gadget *gadgets.Gadget) error {
			saved = gadget
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(gadgets.Gadget{ID: "g1", Description: "Test Gadget", Price: 29.99})
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
	if saved.ID != "g1" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "g1")
	}
}

func TestSaveGRPC_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, gadget *gadgets.Gadget) error {
			t.Error("save should not be called")
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: []byte(`not json`)})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveGRPC_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, gadget *gadgets.Gadget) error {
			return errors.New("state store unavailable")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(gadgets.Gadget{ID: "g1"})
	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: data})

	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestSaveSDK_Success(t *testing.T) {
	t.Parallel()

	var saved *gadgets.Gadget
	store := &mockStore{
		saveFunc: func(ctx context.Context, gadget *gadgets.Gadget) error {
			saved = gadget
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(gadgets.Gadget{ID: "g2", Description: "SDK Gadget", Price: 12.5})
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
	if saved.ID != "g2" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "g2")
	}
}

func TestSaveSDK_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, gadget *gadgets.Gadget) error {
			return errors.New("store error")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(gadgets.Gadget{ID: "g3"})
	event := &common.TopicEvent{RawData: data}

	retry, err := svc.SaveSDK(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from store")
	}
	if retry {
		t.Error("retry should be false")
	}
}
