package dapr_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/gofiber/fiber/v3"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
)

// fakeService records that RegisterService was called and registers a single
// handler so we can assert through the Fiber app afterwards.
type fakeService struct {
	called bool
}

func (f *fakeService) RegisterService(app *fiber.App) {
	f.called = true
	app.Get("/svc/ping", func(c fiber.Ctx) error {
		return c.SendString("pong")
	})
}

type fakeEvents struct {
	called bool
}

func (f *fakeEvents) RegisterEventHandlers(app *fiber.App) {
	f.called = true
	app.Post("/widgets.v1", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})
}

func TestRegisterServices_CallsEachService(t *testing.T) {
	app := fiber.New()
	a := &fakeService{}
	b := &fakeService{}
	dapr.RegisterServices(app, a, b)
	if !a.called || !b.called {
		t.Fatalf("RegisterService not invoked on every Service: a=%v b=%v", a.called, b.called)
	}
	// Verify the registration actually wired a route through the app.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/svc/ping", http.NoBody)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRegisterEventHandlers_CallsEachEvents(t *testing.T) {
	app := fiber.New()
	e := &fakeEvents{}
	dapr.RegisterEventHandlers(app, e)
	if !e.called {
		t.Fatal("RegisterEventHandlers not invoked")
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/widgets.v1", http.NoBody)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestSubscribeHTTPHandler_RespondsWithSubscriptions(t *testing.T) {
	app := fiber.New()
	subs := []*dapr.Subscription{
		{
			PubsubName: "pubsub",
			Topic:      "inventory",
			Routes: dapr.Routes{
				Default: "/default",
				Rules: []dapr.Rule{
					{Match: `event.type == "widget.v1"`, Path: "/widgets.v1"},
				},
			},
		},
	}
	register := dapr.SubscribeHTTPHandler(logr.Discard(), app)
	register(subs)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dapr/subscribe", http.NoBody)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got []dapr.Subscription
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not JSON-array of Subscription: %v\n%s", err, body)
	}
	if len(got) != 1 || got[0].Topic != "inventory" || got[0].Routes.Default != "/default" {
		t.Errorf("subscriptions = %+v", got)
	}
	if len(got[0].Routes.Rules) != 1 || got[0].Routes.Rules[0].Path != "/widgets.v1" {
		t.Errorf("rules = %+v", got[0].Routes.Rules)
	}
}

func TestDecodeCloudEvent_ExtractsDataIntoTarget(t *testing.T) {
	app := fiber.New()
	type widget struct {
		ID    string  `json:"id"`
		Price float64 `json:"price"`
	}

	var captured widget
	var capturedCE dapr.CloudEvent
	app.Post("/widgets.v1", func(c fiber.Ctx) error {
		if err := dapr.DecodeCloudEvent(c, &capturedCE, &captured); err != nil {
			return c.Status(http.StatusBadRequest).SendString(err.Error())
		}
		return c.SendStatus(http.StatusNoContent)
	})

	body := `{
		"specversion":"1.0",
		"type":"widget.v1",
		"source":"test",
		"id":"e-1",
		"datacontenttype":"application/json",
		"data":{"id":"w-1","price":9.99}
	}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/widgets.v1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/cloudevents+json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	if captured.ID != "w-1" || captured.Price != 9.99 {
		t.Errorf("decoded data = %+v", captured)
	}
	if capturedCE.Type != "widget.v1" || capturedCE.ID != "e-1" {
		t.Errorf("envelope = %+v", capturedCE)
	}
}
