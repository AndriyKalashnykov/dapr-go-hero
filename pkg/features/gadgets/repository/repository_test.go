package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets"
)

// mockStateStore implements state.Store for testing.
type mockStateStore struct {
	setStateFunc func(ctx context.Context, store string, items ...state.Item) error
	getStateFunc func(ctx context.Context, store, key string, target interface{}) error
}

func (m *mockStateStore) SetState(ctx context.Context, store string, items ...state.Item) error {
	return m.setStateFunc(ctx, store, items...)
}

func (m *mockStateStore) GetState(ctx context.Context, store, key string, target interface{}) error {
	return m.getStateFunc(ctx, store, key, target)
}

func TestNew(t *testing.T) {
	t.Parallel()

	repo := New(logr.Discard(), &mockStateStore{}, "statestore")
	if repo == nil {
		t.Fatal("New returned nil")
	}
}

func TestSave_Success(t *testing.T) {
	t.Parallel()

	var capturedStore string
	var capturedItems []state.Item

	store := &mockStateStore{
		setStateFunc: func(ctx context.Context, store string, items ...state.Item) error {
			capturedStore = store
			capturedItems = items
			return nil
		},
	}

	repo := New(logr.Discard(), store, "statestore")
	gadget := &gadgets.Gadget{ID: "g1", Description: "Test Gadget", Price: 19.99}

	err := repo.Save(context.Background(), gadget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedStore != "statestore" {
		t.Errorf("store = %q, want %q", capturedStore, "statestore")
	}
	if len(capturedItems) != 1 {
		t.Fatalf("got %d items, want 1", len(capturedItems))
	}
	if capturedItems[0].Key != "gadget:g1" {
		t.Errorf("Key = %q, want %q", capturedItems[0].Key, "gadget:g1")
	}
}

func TestSave_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStateStore{
		setStateFunc: func(ctx context.Context, store string, items ...state.Item) error {
			return errorz.Internal(errors.New("redis down"), "set state failed")
		},
	}

	repo := New(logr.Discard(), store, "statestore")
	err := repo.Save(context.Background(), &gadgets.Gadget{ID: "g1"})

	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Parallel()

	store := &mockStateStore{
		getStateFunc: func(ctx context.Context, store, key string, target interface{}) error {
			if key != "gadget:g1" {
				t.Errorf("key = %q, want %q", key, "gadget:g1")
			}
			// Simulate the state store populating the target
			gadget := target.(*gadgets.Gadget)
			gadget.ID = "g1"
			gadget.Description = "Loaded Gadget"
			gadget.Price = 25.0
			return nil
		},
	}

	repo := New(logr.Discard(), store, "statestore")
	gadget, err := repo.Load(context.Background(), "g1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gadget.ID != "g1" {
		t.Errorf("ID = %q, want %q", gadget.ID, "g1")
	}
	if gadget.Description != "Loaded Gadget" {
		t.Errorf("Description = %q", gadget.Description)
	}
	if gadget.Price != 25.0 {
		t.Errorf("Price = %f, want 25.0", gadget.Price)
	}
}

func TestLoad_NotFound(t *testing.T) {
	t.Parallel()

	store := &mockStateStore{
		getStateFunc: func(ctx context.Context, store, key string, target interface{}) error {
			return errorz.NotFound("key %q not found", key)
		},
	}

	repo := New(logr.Discard(), store, "statestore")
	_, err := repo.Load(context.Background(), "nonexistent")

	if err == nil {
		t.Fatal("expected error for missing gadget")
	}
	var errz *errorz.Error
	if !errors.As(err, &errz) {
		t.Fatalf("expected *errorz.Error, got %T", err)
	}
	if errz.Code != 404 {
		t.Errorf("Code = %d, want 404", errz.Code)
	}
}

func TestLoad_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockStateStore{
		getStateFunc: func(ctx context.Context, store, key string, target interface{}) error {
			return errorz.Internal(errors.New("timeout"), "state store error")
		},
	}

	repo := New(logr.Discard(), store, "statestore")
	_, err := repo.Load(context.Background(), "g1")

	if err == nil {
		t.Fatal("expected error from store")
	}
	var errz *errorz.Error
	if !errors.As(err, &errz) {
		t.Fatalf("expected *errorz.Error, got %T", err)
	}
	if errz.Code != 500 {
		t.Errorf("Code = %d, want 500", errz.Code)
	}
}
