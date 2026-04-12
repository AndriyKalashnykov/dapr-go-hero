package dapr

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCloudEvent_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	original := CloudEvent{
		ID:              "evt-123",
		Source:          "test-source",
		SpecVersion:     "1.0",
		Type:            "widget.v1",
		DataContentType: "application/json",
		DataSchema:      "https://example.com/schema",
		Subject:         "widget-1",
		Time:            now,
		Data:            json.RawMessage(`{"id":"w1","description":"Test","price":9.99}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded CloudEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source = %q, want %q", decoded.Source, original.Source)
	}
	if decoded.SpecVersion != original.SpecVersion {
		t.Errorf("SpecVersion = %q, want %q", decoded.SpecVersion, original.SpecVersion)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, original.Type)
	}
	if decoded.DataContentType != original.DataContentType {
		t.Errorf("DataContentType = %q, want %q", decoded.DataContentType, original.DataContentType)
	}
	if decoded.DataSchema != original.DataSchema {
		t.Errorf("DataSchema = %q, want %q", decoded.DataSchema, original.DataSchema)
	}
	if decoded.Subject != original.Subject {
		t.Errorf("Subject = %q, want %q", decoded.Subject, original.Subject)
	}
	if !decoded.Time.Equal(original.Time) {
		t.Errorf("Time = %v, want %v", decoded.Time, original.Time)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data = %s, want %s", decoded.Data, original.Data)
	}
}

func TestCloudEvent_OmitsEmptyFields(t *testing.T) {
	t.Parallel()

	minimal := CloudEvent{
		ID:          "evt-1",
		Source:      "src",
		SpecVersion: "1.0",
		Type:        "test",
		Data:        json.RawMessage(`{}`),
	}

	data, err := json.Marshal(minimal)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	for _, field := range []string{"datacontenttype", "dataschema", "subject"} {
		if _, ok := raw[field]; ok {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}

	for _, field := range []string{"id", "source", "specversion", "type", "data"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("field %q should be present", field)
		}
	}
}

func TestCloudEvent_DataUnmarshal(t *testing.T) {
	t.Parallel()

	ce := CloudEvent{
		Data: json.RawMessage(`{"id":"gadget-1","description":"Cool Gadget","price":19.99}`),
	}

	type payload struct {
		ID          string  `json:"id"`
		Description string  `json:"description"`
		Price       float64 `json:"price"`
	}

	var p payload
	if err := json.Unmarshal(ce.Data, &p); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if p.ID != "gadget-1" {
		t.Errorf("ID = %q, want %q", p.ID, "gadget-1")
	}
	if p.Description != "Cool Gadget" {
		t.Errorf("Description = %q", p.Description)
	}
	if p.Price != 19.99 {
		t.Errorf("Price = %f, want 19.99", p.Price)
	}
}
