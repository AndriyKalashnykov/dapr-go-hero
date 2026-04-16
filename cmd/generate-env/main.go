// Command generate-env writes a .env file from pkg/config defaults.
// Run: go run ./cmd/generate-env
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/config"
)

func main() {
	var b strings.Builder
	b.WriteString("# Environment configuration for dapr-go-hero.\n")
	b.WriteString("# Loaded at startup by pkg/config loadDotenv — existing env vars take precedence.\n")
	b.WriteString("# Regenerate with: make generate-env\n")

	group := ""
	for _, e := range config.Defaults() {
		// Group by comment prefix
		newGroup := groupName(e.Comment)
		if newGroup != group {
			b.WriteString("\n# " + newGroup + "\n")
			group = newGroup
		}
		b.WriteString(e.Key + "=" + e.Default + "\n")
	}

	if err := os.WriteFile(".env", []byte(b.String()), 0o644); err != nil { // #nosec G306
		fmt.Fprintf(os.Stderr, "error writing .env: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(".env generated from pkg/config defaults.")
}

func groupName(comment string) string {
	switch {
	case strings.Contains(comment, "Inventory"), strings.Contains(comment, "Fiber"),
		strings.Contains(comment, "Custom"), strings.Contains(comment, "SDK"):
		return "Inventory service"
	case strings.Contains(comment, "Products"):
		return "Products service"
	case strings.Contains(comment, "Dapr"):
		return "Dapr sidecar (set automatically by dapr run / K8s sidecar injector)"
	default:
		return "Other"
	}
}
