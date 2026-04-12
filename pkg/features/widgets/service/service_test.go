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
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/widgets"
)

// mockStore implements widgets.Store for testing.
type mockStore struct {
	saveFunc func(ctx context.Context, widget *widgets.Widget) error
	loadFunc func(ctx context.Context, id string) (*widgets.Widget, error)
}

func (m *mockStore) Save(ctx context.Context, widget *widgets.Widget) error {
	return m.saveFunc(ctx, widget)
}

func (m *mockStore) Load(ctx context.Context, id string) (*widgets.Widget, error) {
	return m.loadFunc(ctx, id)
}

func TestNew(t *testing.T) {
	t.Parallel()

	store := &mockStore{}
	svc := New(logr.Discard(), store)

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
		t.Errorf("PubsubName = %q, want %q", subs[0].PubsubName, "pubsub")
	}
	if subs[0].Topic != "inventory" {
		t.Errorf("Topic = %q, want %q", subs[0].Topic, "inventory")
	}
	if len(subs[0].Routes.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(subs[0].Routes.Rules))
	}
	if subs[0].Routes.Rules[0].Match != `event.type == "widget.v1"` {
		t.Errorf("Match = %q", subs[0].Routes.Rules[0].Match)
	}
	if subs[0].Routes.Rules[0].Path != "/widgets.v1" {
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

	if registeredPath != "/widgets.v1" {
		t.Errorf("registered path = %q, want %q", registeredPath, "/widgets.v1")
	}
}

func TestSaveGRPC_Success(t *testing.T) {
	t.Parallel()

	var saved *widgets.Widget
	store := &mockStore{
		saveFunc: func(ctx context.Context, widget *widgets.Widget) error {
			saved = widget
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(widgets.Widget{ID: "w1", Description: "Test Widget", Price: 9.99})
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
	if saved.ID != "w1" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "w1")
	}
	if saved.Description != "Test Widget" {
		t.Errorf("saved.Description = %q", saved.Description)
	}
	if saved.Price != 9.99 {
		t.Errorf("saved.Price = %f, want 9.99", saved.Price)
	}
}

func TestSaveGRPC_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, widget *widgets.Widget) error {
			t.Error("save should not be called on invalid JSON")
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: []byte(`{invalid}`)})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveGRPC_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, widget *widgets.Widget) error {
			return errors.New("db connection lost")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(widgets.Widget{ID: "w1"})
	_, err := svc.SaveGRPC(context.Background(), &pb.TopicEventRequest{Data: data})

	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestSaveSDK_Success(t *testing.T) {
	t.Parallel()

	var saved *widgets.Widget
	store := &mockStore{
		saveFunc: func(ctx context.Context, widget *widgets.Widget) error {
			saved = widget
			return nil
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(widgets.Widget{ID: "w2", Description: "SDK Widget", Price: 15.5})
	event := &common.TopicEvent{
		RawData: data,
	}

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
	if saved.ID != "w2" {
		t.Errorf("saved.ID = %q, want %q", saved.ID, "w2")
	}
}

func TestSaveSDK_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStore{
		saveFunc: func(ctx context.Context, widget *widgets.Widget) error {
			return errors.New("store error")
		},
	}
	svc := New(logr.Discard(), store)

	data, _ := json.Marshal(widgets.Widget{ID: "w3"})
	event := &common.TopicEvent{
		RawData: data,
	}

	retry, err := svc.SaveSDK(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from store")
	}
	if retry {
		t.Error("retry should be false")
	}
}
