package dapr

import (
	"testing"

	"github.com/go-logr/logr"
)

// stubSubscriber implements Subscriber for testing.
type stubSubscriber struct {
	subs []Subscription
}

func (s *stubSubscriber) Subscriptions() []Subscription {
	return s.subs
}

func TestSubscribe_SingleSubscriber(t *testing.T) {
	t.Parallel()

	sub := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "orders",
				Routes: Routes{
					Rules: []Rule{{Match: `event.type == "order.v1"`, Path: "/orders.v1"}},
				},
			},
		},
	}

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, sub)

	if len(result) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(result))
	}
	if result[0].PubsubName != "pubsub" {
		t.Errorf("PubsubName = %q, want %q", result[0].PubsubName, "pubsub")
	}
	if result[0].Topic != "orders" {
		t.Errorf("Topic = %q, want %q", result[0].Topic, "orders")
	}
	if len(result[0].Routes.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(result[0].Routes.Rules))
	}
	if result[0].Routes.Rules[0].Path != "/orders.v1" {
		t.Errorf("Rule Path = %q, want %q", result[0].Routes.Rules[0].Path, "/orders.v1")
	}
}

func TestSubscribe_MergesSamePubsubTopic(t *testing.T) {
	t.Parallel()

	widgets := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes: Routes{
					Rules: []Rule{{Match: `event.type == "widget.v1"`, Path: "/widgets.v1"}},
				},
			},
		},
	}
	gadgets := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes: Routes{
					Rules: []Rule{{Match: `event.type == "gadget.v1"`, Path: "/gadgets.v1"}},
				},
			},
		},
	}

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, widgets, gadgets)

	if len(result) != 1 {
		t.Fatalf("got %d subscriptions, want 1 (should merge same pubsub:topic)", len(result))
	}
	if len(result[0].Routes.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(result[0].Routes.Rules))
	}
	if result[0].Routes.Rules[0].Path != "/widgets.v1" {
		t.Errorf("Rule[0] Path = %q, want %q", result[0].Routes.Rules[0].Path, "/widgets.v1")
	}
	if result[0].Routes.Rules[1].Path != "/gadgets.v1" {
		t.Errorf("Rule[1] Path = %q, want %q", result[0].Routes.Rules[1].Path, "/gadgets.v1")
	}
}

func TestSubscribe_DifferentTopicsNotMerged(t *testing.T) {
	t.Parallel()

	sub := &stubSubscriber{
		subs: []Subscription{
			{PubsubName: "pubsub", Topic: "orders"},
			{PubsubName: "pubsub", Topic: "inventory"},
		},
	}

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, sub)

	if len(result) != 2 {
		t.Fatalf("got %d subscriptions, want 2", len(result))
	}
}

func TestSubscribe_DefaultRouteSet(t *testing.T) {
	t.Parallel()

	sub := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes:     Routes{Default: "/products.v1"},
			},
		},
	}

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, sub)

	if len(result) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(result))
	}
	if result[0].Routes.Default != "/products.v1" {
		t.Errorf("Default = %q, want %q", result[0].Routes.Default, "/products.v1")
	}
}

func TestSubscribe_DuplicateDefaultRouteLogs(t *testing.T) {
	t.Parallel()

	// Two subscribers both setting default route on the same pubsub:topic
	sub1 := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes:     Routes{Default: "/first"},
			},
		},
	}
	sub2 := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes:     Routes{Default: "/second"},
			},
		},
	}

	var result []*Subscription
	// logr.Discard() swallows the error log — we just verify the last-write-wins behavior
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, sub1, sub2)

	if len(result) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(result))
	}
	// Last subscriber's default wins
	if result[0].Routes.Default != "/second" {
		t.Errorf("Default = %q, want %q (last-write-wins)", result[0].Routes.Default, "/second")
	}
}

func TestSubscribe_MergesRulesAndDefault(t *testing.T) {
	t.Parallel()

	widgets := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes: Routes{
					Rules: []Rule{{Match: `event.type == "widget.v1"`, Path: "/widgets.v1"}},
				},
			},
		},
	}
	products := &stubSubscriber{
		subs: []Subscription{
			{
				PubsubName: "pubsub",
				Topic:      "inventory",
				Routes:     Routes{Default: "/products.v1"},
			},
		},
	}

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	}, widgets, products)

	if len(result) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(result))
	}
	if len(result[0].Routes.Rules) != 1 {
		t.Errorf("got %d rules, want 1", len(result[0].Routes.Rules))
	}
	if result[0].Routes.Default != "/products.v1" {
		t.Errorf("Default = %q, want %q", result[0].Routes.Default, "/products.v1")
	}
}

func TestSubscribe_NoSubscribers(t *testing.T) {
	t.Parallel()

	var result []*Subscription
	Subscribe(logr.Discard(), func(subs []*Subscription) {
		result = subs
	})

	if len(result) != 0 {
		t.Fatalf("got %d subscriptions, want 0", len(result))
	}
}
