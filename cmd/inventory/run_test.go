package main

import (
	"testing"
)

// TestSelectClient_DispatchMatrix exhausts every branch of the
// clientType-string switch in selectClient. The label is the human-readable
// name we log; isDefault distinguishes a deliberate "no arg → sdk" from
// "garbage arg → fall back to sdk" (the latter is what happens when a
// typo'd CLI arg should still produce a usable startup, with a log line
// the operator can investigate).
func TestSelectClient_DispatchMatrix(t *testing.T) {
	tests := []struct {
		clientType    string
		wantLabel     string
		wantIsDefault bool
	}{
		{"http", "http", false},
		{"grpc", "grpc", false},
		{"sdk", "sdk", false},
		{"", "sdk", true},             // unset → fall back to sdk; flag the fallback
		{"garbage-typo", "sdk", true}, // unrecognized → sdk default; flag the fallback
		{"HTTP", "sdk", true},         // case-sensitive (intentional — no surprise normalization)
		{"http ", "sdk", true},        // trailing whitespace not normalized
	}
	for _, tt := range tests {
		t.Run("clientType="+tt.clientType, func(t *testing.T) {
			label, ctor, isDefault := selectClient(tt.clientType)
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
			if isDefault != tt.wantIsDefault {
				t.Errorf("isDefault = %v, want %v", isDefault, tt.wantIsDefault)
			}
			if ctor == nil {
				t.Errorf("ctor is nil — selectClient must always return a constructor")
			}
		})
	}
}
