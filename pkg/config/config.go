// Package config provides environment-based configuration with sensible defaults.
// All host/port values are configurable for both local development and Kubernetes deployment.
//
// On startup, a .env file in the working directory is loaded (if present).
// Existing environment variables take precedence — .env never overwrites them.
package config

import (
	"bufio"
	"os"
	"strings"
)

// defaults is the single source of truth for every config key and its default value.
// Used at runtime (env fallback) and by GenerateDotenv to keep .env in sync.
var defaults = []entry{
	// Inventory service
	{"PUBLIC_API_ADDR", ":3000", "Fiber REST API listen address"},
	{"CUSTOM_HTTP_ADDR", ":3001", "Custom HTTP Dapr callback listen address"},
	{"CUSTOM_GRPC_ADDR", "0.0.0.0:4001", "Custom gRPC Dapr callback listen address"}, // #nosec G102
	{"SDK_HTTP_ADDR", ":3002", "Dapr Go SDK HTTP service listen address"},
	{"SDK_GRPC_ADDR", ":4002", "Dapr Go SDK gRPC service listen address"},

	// Products service
	{"PRODUCTS_ADDR", "0.0.0.0:50151", "Products gRPC service listen address"}, // #nosec G102

	// Dapr sidecar (set automatically by dapr run / K8s sidecar injector)
	{"DAPR_HTTP_PORT", "3500", "Dapr sidecar HTTP API port"},
	{"DAPR_GRPC_PORT", "50001", "Dapr sidecar gRPC API port"},

	// Dapr app IDs — use "<id>.<namespace>" for cross-namespace K8s invocation
	{"PRODUCTS_APP_ID", "products", "Dapr app ID of the products service"},
}

type entry struct {
	Key     string
	Default string
	Comment string
}

func init() {
	// Load .env if present; never overwrites existing env vars.
	loadDotenv(".env")
}

// loadDotenv parses a KEY=VALUE file and sets each key in the process
// environment only if it is unset. Unquotes surrounding "" or ” and
// skips blank lines and comments (`#`). Silent on missing files.
func loadDotenv(path string) {
	f, err := os.Open(path) // #nosec G304 -- path comes from trusted caller (init: hardcoded ".env", tests: t.TempDir())
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[0] == val[len(val)-1] {
			val = val[1 : len(val)-1]
		}
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, val)
		}
	}
}

// env returns the value of the named environment variable, or fallback if unset/empty.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Inventory service ports.
var (
	PublicAPIAddr  = env("PUBLIC_API_ADDR", ":3000")
	CustomHTTPAddr = env("CUSTOM_HTTP_ADDR", ":3001")
	CustomGRPCAddr = env("CUSTOM_GRPC_ADDR", "0.0.0.0:4001") // #nosec G102 -- K8s requires 0.0.0.0
	SDKHTTPAddr    = env("SDK_HTTP_ADDR", ":3002")
	SDKGRPCAddr    = env("SDK_GRPC_ADDR", ":4002")
)

// Products service.
var (
	ProductsAddr = env("PRODUCTS_ADDR", "0.0.0.0:50151") // #nosec G102 -- K8s requires 0.0.0.0
)

// Dapr sidecar ports.
var (
	DaprHTTPPort = env("DAPR_HTTP_PORT", "3500")
	DaprGRPCPort = env("DAPR_GRPC_PORT", "50001")
)

// Dapr app IDs for service invocation. In K8s with services in separate
// namespaces, use fully-qualified IDs ("<app-id>.<namespace>").
var (
	ProductsAppID = env("PRODUCTS_APP_ID", "products")
)

// Defaults returns all config entries for external tooling (e.g., .env generation).
func Defaults() []struct {
	Key, Default, Comment string
} {
	out := make([]struct{ Key, Default, Comment string }, len(defaults))
	for i, e := range defaults {
		out[i] = struct{ Key, Default, Comment string }{e.Key, e.Default, e.Comment}
	}
	return out
}
