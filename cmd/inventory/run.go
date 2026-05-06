package main

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/cenkalti/backoff/v5"
	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/go-sdk/service/common"
	dapr_server_grpc "github.com/dapr/go-sdk/service/grpc"
	dapr_server_http "github.com/dapr/go-sdk/service/http"
	"github.com/go-logr/logr"
	"github.com/gofiber/fiber/v3"
	oklogrun "github.com/oklog/run"
	"google.golang.org/grpc"

	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/secrets"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/components/state"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/config"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/connect/postgres"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/dapr"
	"github.com/AndriyKalashnykov/dapr-go-hero/pkg/errorz"
	gadgets_repo "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets/repository"
	gadgets_service "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/gadgets/service"
	products_repo "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products/repository"
	products_service "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/products/service"
	widgets_repo "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/widgets/repository"
	widgets_service "github.com/AndriyKalashnykov/dapr-go-hero/pkg/features/widgets/service"
)

// api is an interface to embed all the components.
type api interface {
	secrets.Store
	state.Store
	Name() string
}

// clientConstructor builds a Dapr client. Extracted so selectClient is a
// pure dispatch function (no I/O until the constructor is called) and
// tests can substitute a fake without spawning a real sidecar.
type clientConstructor func(context.Context) (api, error)

const clientLabelSDK = "sdk"

// selectClient maps a clientType ("http" / "grpc" / anything-else) to a
// human-readable label + the matching Dapr client constructor. The third
// return value documents the default-fallback behavior so a test can
// distinguish "the user asked for sdk" from "the user passed garbage and
// we fell back to sdk."
//
// This is the Pattern-B extraction from /test-coverage-analysis: minimal
// pure-dispatch surface, fully unit-testable, no infrastructure required.
func selectClient(clientType string) (label string, ctor clientConstructor, isDefault bool) {
	switch clientType {
	case "http":
		return "http", func(ctx context.Context) (api, error) { return dapr.NewHTTP(ctx) }, false
	case "grpc":
		return "grpc", func(ctx context.Context) (api, error) { return dapr.NewGRPC(ctx) }, false
	case clientLabelSDK, "":
		return clientLabelSDK, func(ctx context.Context) (api, error) { return dapr.NewSDK(ctx) }, clientType == ""
	default:
		return clientLabelSDK, func(ctx context.Context) (api, error) { return dapr.NewSDK(ctx) }, true
	}
}

// dialDaprClient wraps a constructor in the project's standard backoff
// policy. Extracted so the retry policy is testable without rebuilding the
// goroutine wiring around it. backoff.Operation[T] is `func() (T, error)`
// — bind ctx via closure so the constructor signature stays
// (context-taking) and unit tests can pass a no-arg fake.
func dialDaprClient(ctx context.Context, log logr.Logger, ctor clientConstructor) (api, error) {
	op := func() (api, error) { return ctor(ctx) }
	return backoff.Retry(ctx, op, backoff.WithNotify(func(err error, _ time.Duration) {
		log.Info("Retrying Dapr client connection...")
	}))
}

// run is the testable entrypoint extracted from main. Returns nil on clean
// shutdown (signal received) or an error suitable for printing + exit-1.
//
// args is the positional argument list (not flag values) — main passes
// flag.Args() so tests can drive the dispatch matrix with literal slices
// instead of mutating os.Args.
func run(ctx context.Context, args []string, log logr.Logger) error {
	clientType := ""
	if len(args) > 0 {
		clientType = args[0]
	}

	label, ctor, _ := selectClient(clientType)
	log.Info("Selected Dapr client", "clientType", label)

	daprClient, err := dialDaprClient(ctx, log, ctor)
	if err != nil {
		return errorz.Internal(err, "could not create connection to Dapr")
	}
	log.Info("Client initialized", "name", daprClient.Name())

	// Connect to database
	pool, err := postgres.Connect(ctx, daprClient,
		"secrets", "postgres",
		widgets_repo.AfterConnect)
	if err != nil {
		return errorz.Internal(err, "could not create connection to Postgres")
	}
	defer pool.Close()

	// Wire up dependencies.
	widgetRepo := widgets_repo.New(log, pool)
	widgetRest := widgets_service.New(log, widgetRepo)

	gadgetRepo := gadgets_repo.New(log, daprClient, "statestore")
	gadgetRest := gadgets_service.New(log, gadgetRepo)

	productRepo, err := products_repo.New(log)
	if err != nil {
		return errorz.Internal(err, "could not create connection to Products service")
	}
	defer func() { _ = productRepo.Close() }()

	productRest := products_service.New(log, productRepo)

	// Fiber app config with custom error handler.
	fiberCfg := fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			errz := errorz.From(err)
			return c.Status(errz.Code).JSON(errz)
		},
	}

	var g oklogrun.Group
	// Public REST API.
	{
		app := fiber.New(fiberCfg)
		dapr.RegisterServices(app,
			widgetRest, gadgetRest, productRest)
		g.Add(func() error {
			return app.Listen(config.PublicAPIAddr)
		}, func(err error) {
			_ = app.Shutdown()
		})
	}
	// Custom HTTP event handlers.
	{
		app := fiber.New(fiberCfg)
		dapr.RegisterEventHandlers(app,
			widgetRest, gadgetRest, productRest)
		dapr.Subscribe(log, dapr.SubscribeHTTPHandler(log, app),
			widgetRest, gadgetRest, productRest)
		g.Add(func() error {
			return app.Listen(config.CustomHTTPAddr)
		}, func(err error) {
			_ = app.Shutdown()
		})
	}
	// Custom gRPC event handlers.
	{
		gs := grpc.NewServer()
		server := dapr.NewServer(log)
		server.RegisterTopicEventHandlers(
			widgetRest, gadgetRest, productRest)
		dapr.Subscribe(log, server.Subscribe,
			widgetRest, gadgetRest, productRest)
		pb.RegisterAppCallbackServer(gs, server)
		g.Add(func() error {
			lc := &net.ListenConfig{}
			ln, err := lc.Listen(ctx, "tcp", config.CustomGRPCAddr) // #nosec G102 -- configurable via CUSTOM_GRPC_ADDR
			if err != nil {
				return err
			}
			return gs.Serve(ln)
		}, func(err error) {
			gs.GracefulStop()
		})
	}
	// SDK HTTP event handlers.
	{
		var s common.Service
		g.Add(func() error {
			s = dapr_server_http.NewService(config.SDKHTTPAddr)
			if err := errors.Join(
				widgetRest.RegisterTopicEventHandlersSDK(s),
				gadgetRest.RegisterTopicEventHandlersSDK(s),
				productRest.RegisterTopicEventHandlersSDK(s)); err != nil {
				return err
			}
			return s.Start()
		}, func(err error) {
			if s != nil {
				_ = s.Stop()
			}
		})
	}
	// SDK gRPC event handlers.
	{
		var s common.Service
		g.Add(func() error {
			var err error
			s, err = dapr_server_grpc.NewService(config.SDKGRPCAddr)
			if err != nil {
				return err
			}
			if err := errors.Join(
				widgetRest.RegisterTopicEventHandlersSDK(s),
				gadgetRest.RegisterTopicEventHandlersSDK(s),
				productRest.RegisterTopicEventHandlersSDK(s)); err != nil {
				return err
			}
			return s.Start()
		}, func(err error) {
			if s != nil {
				_ = s.Stop()
			}
		})
	}
	// Termination signals.
	g.Add(oklogrun.SignalHandler(ctx, os.Interrupt, os.Kill))

	if err := g.Run(); err != nil {
		var se oklogrun.SignalError
		if errors.As(err, &se) {
			// Clean signal-driven shutdown.
			return nil
		}
		return errorz.Internal(err, "goroutine error")
	}
	return nil
}
