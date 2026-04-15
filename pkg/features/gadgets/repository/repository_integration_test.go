//go:build integration

// Integration test for the gadgets repository against a stub Dapr sidecar.
// The stub impersonates the Dapr state API (POST /v1.0/state/{store},
// GET /v1.0/state/{store}/{key}) so we can assert the on-the-wire request
// shape the HTTP client actually produces.
package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets/repository"
)

// stubSidecar captures the Dapr state API traffic and serves canned responses.
type stubSidecar struct {
	mu      sync.Mutex
	store   map[string]json.RawMessage
	saveReq []stateSaveRequest
}

type stateSaveRequest struct {
	Store string
	Items []map[string]any
}

func newStubSidecar(t *testing.T) (*stubSidecar, string) {
	t.Helper()
	s := &stubSidecar{store: map[string]json.RawMessage{}}

	// Bind to an ephemeral port so parallel tests can't collide.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/state/", s.handle)

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	u := &url.URL{Scheme: "http", Host: ln.Addr().String(), Path: "/"}
	return s, u.String()
}

func (s *stubSidecar) handle(w http.ResponseWriter, r *http.Request) {
	// /v1.0/state/{store}           (POST: batch upsert)
	// /v1.0/state/{store}/{key}     (GET)
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1.0/state/")
	parts := strings.SplitN(trimmed, "/", 2)

	switch {
	case r.Method == http.MethodPost && len(parts) == 1:
		var items []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.saveReq = append(s.saveReq, stateSaveRequest{Store: parts[0], Items: items})
		for _, it := range items {
			key, _ := it["key"].(string)
			val, _ := json.Marshal(it["value"])
			s.store[parts[0]+"/"+key] = val
		}
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodGet && len(parts) == 2:
		s.mu.Lock()
		defer s.mu.Unlock()
		raw, ok := s.store[parts[0]+"/"+parts[1]]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)

	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

// withStubSidecar points the package-level dapr.APIURL at the stub, and
// restores it on cleanup. The HTTP client constructs URLs by concatenating
// APIURL + "v1.0/state/..." so this is the single integration point.
func withStubSidecar(t *testing.T, base string) {
	t.Helper()
	orig := dapr.APIURL
	dapr.APIURL = base
	t.Cleanup(func() { dapr.APIURL = orig })
}

func newRepo(t *testing.T) (*repository.Repository, *stubSidecar) {
	t.Helper()
	stub, base := newStubSidecar(t)
	withStubSidecar(t, base)

	client, err := dapr.NewHTTP(context.Background())
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	return repository.New(logr.Discard(), client, "statestore"), stub
}

func TestRepository_Save_SendsCorrectDaprStateRequest(t *testing.T) {
	repo, stub := newRepo(t)
	ctx := context.Background()

	g := &gadgets.Gadget{ID: "g1", Description: "Hello", Price: 1.5}
	if err := repo.Save(ctx, g); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.saveReq) != 1 {
		t.Fatalf("expected 1 save request, got %d", len(stub.saveReq))
	}
	req := stub.saveReq[0]
	if req.Store != "statestore" {
		t.Errorf("store = %q, want %q", req.Store, "statestore")
	}
	if len(req.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(req.Items))
	}
	if got := req.Items[0]["key"]; got != "gadget:g1" {
		t.Errorf("key = %v, want %q", got, "gadget:g1")
	}
	body, _ := io.ReadAll(strings.NewReader(mustJSON(req.Items[0]["value"])))
	if !strings.Contains(string(body), `"description":"Hello"`) {
		t.Errorf("value body missing description: %s", body)
	}
}

func TestRepository_Load_RoundTripsSavedValue(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	want := &gadgets.Gadget{ID: "g2", Description: "Round-trip", Price: 2.5}
	if err := repo.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Load(ctx, "g2")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != want.ID || got.Description != want.Description || got.Price != want.Price {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestRepository_Load_ReturnsNotFoundForMissing(t *testing.T) {
	repo, _ := newRepo(t)

	_, err := repo.Load(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing gadget")
	}
	var ez *errorz.Error
	if !errors.As(err, &ez) {
		t.Fatalf("expected *errorz.Error, got %T: %v", err, err)
	}
	if ez.Code != 404 {
		t.Errorf("Code = %d, want 404", ez.Code)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
