package main

import (
	"context"
	"flag"
	"os"

	"go.uber.org/zap"

	zaplog "github.com/AndriyKalashnykov/dapr-go-hero/pkg/log"
)

// main is intentionally thin so that all real work lives in run() — see
// /test-coverage-analysis Pattern C: keep main() as a fixed shell that
// parses args, builds the logger, and exits on whatever run() returns.
// The dispatch logic, retry policy, repo wiring, and goroutine-group
// lifecycle are all in run.go where they're testable in isolation.
func main() {
	zapLog, err := zap.NewDevelopment()
	if err != nil {
		// No logger yet — write to stderr and exit.
		_, _ = os.Stderr.WriteString("zap.NewDevelopment: " + err.Error() + "\n")
		os.Exit(1)
	}
	log := zaplog.NewLogger(zapLog)

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	// Explicit cleanup before os.Exit (gocritic exitAfterDefer); no defer
	// because os.Exit skips deferred calls.
	exitCode := 0
	if err := run(ctx, flag.Args(), log); err != nil {
		log.Error(err, "run failed")
		exitCode = 1
	}
	cancel()
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
