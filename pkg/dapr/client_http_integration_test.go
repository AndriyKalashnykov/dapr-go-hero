//go:build integration

// Integration test for the custom HTTP Dapr client. A stub server
// impersonates the Dapr sidecar HTTP API (state + secrets) so we can verify
// the client's URL layout, HTTP method, and body shape without pulling in
// the real sidecar.
package dapr_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
)

type stubSidecar struct {
	mu sync.Mutex

	lastStateMethod string
	lastStatePath   string
	lastStateBody   []byte

	lastSecretPath string

	stateValue  []byte
	secretValue []byte

	stateCode  int
	secretCode int
}

func (s *stubSidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case len(r.URL.Path) >= len("/v1.0/state/") && r.URL.Path[:len("/v1.0/state/")] == "/v1.0/state/":
		s.lastStateMethod = r.Method
		s.lastStatePath = r.URL.Path
		if r.Method == http.MethodPost {
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			s.lastStateBody = buf
			code := s.stateCode
			if code == 0 {
				code = http.StatusNoContent
			}
			w.WriteHeader(code)
			return
		}
		// GET
		code := s.stateCode
		if code == 0 {
			code = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(s.stateValue)

	case len(r.URL.Path) >= len("/v1.0/secrets/") && r.URL.Path[:len("/v1.0/secrets/")] == "/v1.0/secrets/":
		s.lastSecretPath = r.URL.Path
		code := s.secretCode
		if code == 0 {
			code = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(s.secretValue)

	default:
		http.NotFound(w, r)
	}
}

func startStub(t *testing.T, s *stubSidecar) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: s}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	orig := dapr.APIURL
	dapr.APIURL = (&url.URL{Scheme: "http", Host: ln.Addr().String(), Path: "/"}).String()
	t.Cleanup(func() { dapr.APIURL = orig })
}

func newHTTPClient(t *testing.T, s *stubSidecar) *dapr.HTTP {
	t.Helper()
	startStub(t, s)
	c, err := dapr.NewHTTP(context.Background())
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	return c
}

func TestHTTP_SetState_PostsBatchToCorrectURL(t *testing.T) {
	stub := &stubSidecar{}
	c := newHTTPClient(t, stub)

	payload := map[string]any{"x": 1}
	err := c.SetState(context.Background(), "statestore",
		state.Item{Key: "foo", Value: payload})
	if err != nil {
		t.Fatalf("SetState: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastStateMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", stub.lastStateMethod)
	}
	if stub.lastStatePath != "/v1.0/state/statestore" {
		t.Errorf("path = %q, want /v1.0/state/statestore", stub.lastStatePath)
	}
	var items []state.Item
	if err := json.Unmarshal(stub.lastStateBody, &items); err != nil {
		t.Fatalf("body not a JSON array of state.Item: %v\n%s", err, stub.lastStateBody)
	}
	if len(items) != 1 || items[0].Key != "foo" {
		t.Errorf("items = %+v", items)
	}
}

func TestHTTP_GetState_DecodesResponseBody(t *testing.T) {
	stub := &stubSidecar{
		stateValue: []byte(`{"ID":"foo","Description":"bar","Price":9.99}`),
	}
	c := newHTTPClient(t, stub)

	var got struct {
		ID          string
		Description string
		Price       float64
	}
	if err := c.GetState(context.Background(), "statestore", "mykey", &got); err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got.ID != "foo" || got.Description != "bar" || got.Price != 9.99 {
		t.Errorf("got %+v", got)
	}
	if stub.lastStatePath != "/v1.0/state/statestore/mykey" {
		t.Errorf("path = %q", stub.lastStatePath)
	}
}

func TestHTTP_GetState_ReturnsNotFoundOn204(t *testing.T) {
	stub := &stubSidecar{stateCode: http.StatusNoContent}
	c := newHTTPClient(t, stub)

	var v any
	err := c.GetState(context.Background(), "statestore", "missing", &v)
	if err == nil {
		t.Fatal("expected error for 204")
	}
}

func TestHTTP_GetSecret_UsesSecretsEndpoint(t *testing.T) {
	stub := &stubSidecar{
		secretValue: []byte(`{"password":"s3cret"}`),
	}
	c := newHTTPClient(t, stub)

	var got map[string]string
	if err := c.GetSecret(context.Background(), "secrets", "postgres", &got); err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got["password"] != "s3cret" {
		t.Errorf("secret = %v", got)
	}
	if stub.lastSecretPath != "/v1.0/secrets/secrets/postgres" {
		t.Errorf("path = %q", stub.lastSecretPath)
	}
}
