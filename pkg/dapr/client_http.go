package dapr

import (
	"context"
	"fmt"
	"path"

	"github.com/gofiber/fiber/v3/client"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/secrets"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/config"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
)

type HTTP struct {
	client *client.Client
}

var (
	APIURL = fmt.Sprintf("http://127.0.0.1:%s/", config.DaprHTTPPort)

	_ = state.Store((*HTTP)(nil))
	_ = secrets.Store((*HTTP)(nil))
)

func NewHTTP(ctx context.Context) (*HTTP, error) {
	c := client.New()
	return &HTTP{client: c}, nil
}

func (c *HTTP) Name() string {
	return "Custom HTTP (using Fiber client)"
}

func (c *HTTP) SetState(ctx context.Context, store string, items ...state.Item) error {
	url := APIURL + path.Join("v1.0/state", store)
	resp, err := c.client.Post(url, client.Config{
		Body: items,
	})
	if err != nil {
		return errorz.Internal(err, "could not save state")
	}
	if resp.StatusCode()/100 != 2 {
		return errorz.Internal(fmt.Errorf("received %d status", resp.StatusCode()), "could not save state")
	}
	return nil
}

func (c *HTTP) GetState(ctx context.Context, store, key string, target interface{}) error {
	url := APIURL + path.Join("v1.0/state", store, key)
	resp, err := c.client.Get(url)
	if err != nil {
		return errorz.Internal(err, "could not load key %q", key)
	}
	code := resp.StatusCode()
	if code == 204 || code == 404 {
		return errorz.NotFound("key %q not found", key)
	}
	if err := resp.JSON(target); err != nil {
		return errorz.Internal(err, "could not parse response for key %q", key)
	}
	return nil
}

func (c *HTTP) GetSecret(ctx context.Context, store, name string, target interface{}) error {
	url := APIURL + path.Join("v1.0/secrets", store, name)
	resp, err := c.client.Get(url)
	if err != nil {
		return errorz.Internal(err, "could not load secret %q", name)
	}
	code := resp.StatusCode()
	if code == 204 || code == 404 {
		return errorz.NotFound("secret %q not found", name)
	}
	if err := resp.JSON(target); err != nil {
		return errorz.Internal(err, "could not parse response for secret %q", name)
	}
	return nil
}
