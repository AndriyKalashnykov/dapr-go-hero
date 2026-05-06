package state_test

import (
	"encoding/json"
	"testing"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
)

// Item is the wire-shape we POST to /v1.0/state/<store> as a JSON array.
// The struct tags are part of the contract; a regression that drops the
// `omitempty` on ETag would cause every SetState call to send `"etag":""`,
// which the Redis state component rejects as a CAS attempt against an
// empty etag.
func TestItem_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   state.Item
		want string
	}{
		{
			"with etag",
			state.Item{Key: "k", Value: map[string]any{"x": 1}, ETag: "v1"},
			`{"key":"k","value":{"x":1},"etag":"v1"}`,
		},
		{
			"empty etag is omitted",
			state.Item{Key: "k", Value: "hello"},
			`{"key":"k","value":"hello"}`,
		},
		{
			"value is nil → null payload (allowed)",
			state.Item{Key: "k"},
			`{"key":"k","value":null}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("\n got %s\nwant %s", got, tt.want)
			}
			// Round-trip: the marshaled form must unmarshal back to an
			// equivalent Item (modulo omitted fields).
			var rt state.Item
			if err := json.Unmarshal(got, &rt); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if rt.Key != tt.in.Key {
				t.Errorf("Key round-trip: got %q, want %q", rt.Key, tt.in.Key)
			}
			if rt.ETag != tt.in.ETag {
				t.Errorf("ETag round-trip: got %q, want %q", rt.ETag, tt.in.ETag)
			}
		})
	}
}
