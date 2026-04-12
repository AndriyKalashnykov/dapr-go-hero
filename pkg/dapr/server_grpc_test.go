package dapr

import (
	"context"
	"errors"
	"testing"

	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/go-logr/logr"
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.handlers == nil {
		t.Error("handlers map not initialized")
	}
	if s.subscriptions == nil {
		t.Error("subscriptions slice not initialized")
	}
}

func TestServer_Subscribe(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	subs := []*Subscription{
		{PubsubName: "pubsub", Topic: "orders"},
		{PubsubName: "pubsub", Topic: "inventory"},
	}
	s.Subscribe(subs)

	if len(s.subscriptions) != 2 {
		t.Fatalf("got %d subscriptions, want 2", len(s.subscriptions))
	}
}

func TestServer_ListTopicSubscriptions(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	s.Subscribe([]*Subscription{
		{
			PubsubName: "pubsub",
			Topic:      "inventory",
			Routes: Routes{
				Rules:   []Rule{{Match: `event.type == "widget.v1"`, Path: "/widgets.v1"}},
				Default: "/default",
			},
			Metadata: map[string]string{"rawPayload": "true"},
		},
	})

	resp, err := s.ListTopicSubscriptions(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTopicSubscriptions error: %v", err)
	}
	if len(resp.Subscriptions) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(resp.Subscriptions))
	}

	sub := resp.Subscriptions[0]
	if sub.PubsubName != "pubsub" {
		t.Errorf("PubsubName = %q, want %q", sub.PubsubName, "pubsub")
	}
	if sub.Topic != "inventory" {
		t.Errorf("Topic = %q, want %q", sub.Topic, "inventory")
	}
	if sub.Routes.Default != "/default" {
		t.Errorf("Routes.Default = %q, want %q", sub.Routes.Default, "/default")
	}
	if len(sub.Routes.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(sub.Routes.Rules))
	}
	if sub.Routes.Rules[0].Match != `event.type == "widget.v1"` {
		t.Errorf("Rule Match = %q", sub.Routes.Rules[0].Match)
	}
	if sub.Routes.Rules[0].Path != "/widgets.v1" {
		t.Errorf("Rule Path = %q", sub.Routes.Rules[0].Path)
	}
	if sub.Metadata["rawPayload"] != "true" {
		t.Errorf("Metadata = %v", sub.Metadata)
	}
}

func TestServer_RegisterTopicEventHandler(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	called := false
	handler := func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		called = true
		return &pb.TopicEventResponse{Status: pb.TopicEventResponse_SUCCESS}, nil
	}

	s.RegisterTopicEventHandler("/test.v1", handler)

	_, _ = s.OnTopicEvent(context.Background(), &pb.TopicEventRequest{Path: "/test.v1"})
	if !called {
		t.Error("registered handler was not called")
	}
}

type stubRegistrar struct {
	path    string
	handler TopicEventHandler
}

func (r *stubRegistrar) RegisterTopicEventHandlers(register RegisterEventHandler) {
	register(r.path, r.handler)
}

func TestServer_RegisterTopicEventHandlers(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	reg1 := &stubRegistrar{path: "/a.v1", handler: func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return &pb.TopicEventResponse{Status: pb.TopicEventResponse_SUCCESS}, nil
	}}
	reg2 := &stubRegistrar{path: "/b.v1", handler: func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return &pb.TopicEventResponse{Status: pb.TopicEventResponse_SUCCESS}, nil
	}}

	s.RegisterTopicEventHandlers(reg1, reg2)

	if _, ok := s.handlers["/a.v1"]; !ok {
		t.Error("handler /a.v1 not registered")
	}
	if _, ok := s.handlers["/b.v1"]; !ok {
		t.Error("handler /b.v1 not registered")
	}
}

func TestServer_OnTopicEvent_Success(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	s.RegisterTopicEventHandler("/widgets.v1", func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return &pb.TopicEventResponse{Status: pb.TopicEventResponse_SUCCESS}, nil
	})

	resp, err := s.OnTopicEvent(context.Background(), &pb.TopicEventRequest{Path: "/widgets.v1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != pb.TopicEventResponse_SUCCESS {
		t.Errorf("Status = %v, want SUCCESS", resp.Status)
	}
}

func TestServer_OnTopicEvent_HandlerNotFound(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	_, err := s.OnTopicEvent(context.Background(), &pb.TopicEventRequest{Path: "/unknown"})
	if err == nil {
		t.Fatal("expected error for unknown path")
	}
	if !errors.Is(err, err) {
		t.Errorf("error = %q", err)
	}
}

func TestServer_OnTopicEvent_HandlerError(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	s.RegisterTopicEventHandler("/fail.v1", func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return nil, errors.New("handler failed")
	})

	_, err := s.OnTopicEvent(context.Background(), &pb.TopicEventRequest{Path: "/fail.v1"})
	if err == nil {
		t.Fatal("expected error from handler")
	}
}

func TestServer_OnBulkTopicEvent(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	s.RegisterTopicEventHandler("/widgets.v1", func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return &pb.TopicEventResponse{Status: pb.TopicEventResponse_SUCCESS}, nil
	})

	resp, err := s.OnBulkTopicEvent(context.Background(), &pb.TopicEventBulkRequest{
		PubsubName: "pubsub",
		Topic:      "inventory",
		Path:       "/widgets.v1",
		Entries: []*pb.TopicEventBulkRequestEntry{
			{
				EntryId: "1",
				Event: &pb.TopicEventBulkRequestEntry_CloudEvent{
					CloudEvent: &pb.TopicEventCERequest{
						Id:          "evt-1",
						Source:      "test",
						Type:        "widget.v1",
						SpecVersion: "1.0",
						Data:        []byte(`{"id":"w1"}`),
					},
				},
			},
			{
				EntryId: "2",
				Event: &pb.TopicEventBulkRequestEntry_CloudEvent{
					CloudEvent: &pb.TopicEventCERequest{
						Id:          "evt-2",
						Source:      "test",
						Type:        "widget.v1",
						SpecVersion: "1.0",
						Data:        []byte(`{"id":"w2"}`),
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(resp.Statuses))
	}
	for i, status := range resp.Statuses {
		if status.Status != pb.TopicEventResponse_SUCCESS {
			t.Errorf("entry[%d] Status = %v, want SUCCESS", i, status.Status)
		}
	}
}

func TestServer_OnBulkTopicEvent_NilCloudEvent(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())

	resp, err := s.OnBulkTopicEvent(context.Background(), &pb.TopicEventBulkRequest{
		PubsubName: "pubsub",
		Topic:      "inventory",
		Path:       "/test",
		Entries: []*pb.TopicEventBulkRequestEntry{
			{
				EntryId: "1",
				// No CloudEvent set — GetCloudEvent returns nil
			},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(resp.Statuses))
	}
	if resp.Statuses[0].Status != pb.TopicEventResponse_DROP {
		t.Errorf("Status = %v, want DROP", resp.Statuses[0].Status)
	}
}

func TestServer_OnBulkTopicEvent_HandlerError(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	s.RegisterTopicEventHandler("/fail.v1", func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
		return nil, errors.New("processing failed")
	})

	resp, err := s.OnBulkTopicEvent(context.Background(), &pb.TopicEventBulkRequest{
		PubsubName: "pubsub",
		Topic:      "inventory",
		Path:       "/fail.v1",
		Entries: []*pb.TopicEventBulkRequestEntry{
			{
				EntryId: "1",
				Event: &pb.TopicEventBulkRequestEntry_CloudEvent{
					CloudEvent: &pb.TopicEventCERequest{
						Id:          "evt-1",
						Source:      "test",
						Type:        "test.v1",
						SpecVersion: "1.0",
						Data:        []byte(`{}`),
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(resp.Statuses))
	}
	if resp.Statuses[0].Status != pb.TopicEventResponse_RETRY {
		t.Errorf("Status = %v, want RETRY", resp.Statuses[0].Status)
	}
}

func TestServer_OnInvoke(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	resp, err := s.OnInvoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
}

func TestServer_ListInputBindings(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	resp, err := s.ListInputBindings(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Bindings) != 0 {
		t.Errorf("got %d bindings, want 0", len(resp.Bindings))
	}
}

func TestServer_OnBindingEvent(t *testing.T) {
	t.Parallel()

	s := NewServer(logr.Discard())
	resp, err := s.OnBindingEvent(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response")
	}
}

func TestConvertRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		routes    Routes
		wantRules int
		wantDef   string
	}{
		{
			name:      "empty routes",
			routes:    Routes{},
			wantRules: 0,
			wantDef:   "",
		},
		{
			name: "rules and default",
			routes: Routes{
				Rules: []Rule{
					{Match: `event.type == "a"`, Path: "/a"},
					{Match: `event.type == "b"`, Path: "/b"},
				},
				Default: "/default",
			},
			wantRules: 2,
			wantDef:   "/default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := convertRoutes(tt.routes)
			if len(got.Rules) != tt.wantRules {
				t.Errorf("got %d rules, want %d", len(got.Rules), tt.wantRules)
			}
			if got.Default != tt.wantDef {
				t.Errorf("Default = %q, want %q", got.Default, tt.wantDef)
			}
		})
	}
}

