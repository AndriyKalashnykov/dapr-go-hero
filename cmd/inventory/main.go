package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"time"

	"github.com/cenkalti/backoff/v5"
	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/go-sdk/service/common"
	dapr_server_grpc "github.com/dapr/go-sdk/service/grpc"
	dapr_server_http "github.com/dapr/go-sdk/service/http"
	zaplog "github.com/AndriyKalashnykov/dapr-go-hero/pkg/log"
	"github.com/gofiber/fiber/v3"
	"github.com/oklog/run"
	"go.uber.org/zap"
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize logger
	zapLog, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	log := zaplog.NewLogger(zapLog)

	clientType := "sdk"

	flag.Parse()
	args := flag.Args()
	if len(args) > 0 {
		clientType = args[0]
	}

	////////////////////////////////////////////////////////
	// For example purposes only, this application can
	// connect to Dapr using:
	//
	//   * Custom code for HTTP
	//   * Custom code for gRPC
	//   * Using the Go SDK (protocol doesn't matter)
	//
	var daprClient api
	daprClient, err = backoff.Retry(ctx, func() (api, error) {
		var client api
		var err error
		switch clientType {
		case "http":
			client, err = dapr.NewHTTP(ctx)
		case "grpc":
			client, err = dapr.NewGRPC(ctx)
		default:
			client, err = dapr.NewSDK(ctx)
		}
		return client, err
	}, backoff.WithNotify(func(err error, _ time.Duration) {
		log.Info("Retrying Dapr client connection...")
	}))
	if err != nil {
		log.Error(err, "could not create connection to Dapr")
		cancel()
		os.Exit(1)
	}
	log.Info("Client initialized", "name", daprClient.Name())

	// Connect to database
	pool, err := postgres.Connect(ctx, daprClient,
		"secrets", "postgres",
		widgets_repo.AfterConnect)
	if err != nil {
		log.Error(err, "could not create connection to Postgres")
		cancel()
		os.Exit(1)
	}

	// Wire up dependencies

	// Uses Postgres database
	widgetRepo := widgets_repo.New(log, pool)
	widgetRest := widgets_service.New(log, widgetRepo)

	// Uses state store
	gadgetRepo := gadgets_repo.New(log, daprClient, "statestore")
	gadgetRest := gadgets_service.New(log, gadgetRepo)

	// Uses service invocation
	productRepo, err := products_repo.New(log)
	if err != nil {
		log.Error(err, "could not create connection to Products service")
		pool.Close()
		cancel()
		os.Exit(1)
	}
	productRest := products_service.New(log, productRepo)

	// Fiber app config with custom error handler
	fiberCfg := fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			errz := errorz.From(err)
			return c.Status(errz.Code).JSON(errz)
		},
	}

	var g run.Group
	// Public REST API operations
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

	////////////////////////////////////////////////////////
	// It is recommended to listen on a different port
	// for private Dapr callbacks. Below are listeners for:
	//
	//   * Custom code for HTTP
	//   * Custom code for gRPC
	//   * Using the SDK for HTTP
	//   * Using the SDK for gRPC
	//

	////////////////////////////////////////////////////////
	// Each of the feature packages will add their own
	// subscriptions. The helper code in
	// /pkg/dapr/subscriptions.go merges them into a single
	// payload response to the Dapr sidecar.
	//
	// It will look like this:
	//
	// subscriptions:
	//   - pubsubname: pubsub
	//     topic: inventory
	//     routes:
	//       rules:
	//         - match: "event.type == 'widget.v1'"
	//           path: /widgets
	//         - match: "event.type == 'gadget.v1'"
	//           path: /gadgets
	//       default: /products
	//

	// Custom - HTTP events handlers
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
	// Custom - gRPC event handlers
	{
		gs := grpc.NewServer()
		server := dapr.NewServer(log)
		server.RegisterTopicEventHandlers(
			widgetRest, gadgetRest, productRest)
		dapr.Subscribe(log, server.Subscribe,
			widgetRest, gadgetRest, productRest)
		pb.RegisterAppCallbackServer(gs, server)
		g.Add(func() error {
			ln, err := net.Listen("tcp", config.CustomGRPCAddr) // #nosec G102 -- configurable via CUSTOM_GRPC_ADDR
			if err != nil {
				return err
			}
			return gs.Serve(ln)
		}, func(err error) {
			gs.GracefulStop()
		})
	}
	// Using SDK - HTTP events handlers
	{
		var s common.Service
		g.Add(func() error {
			s = dapr_server_http.NewService(config.SDKHTTPAddr)
			err = errors.Join(
				widgetRest.RegisterTopicEventHandlersSDK(s),
				gadgetRest.RegisterTopicEventHandlersSDK(s),
				productRest.RegisterTopicEventHandlersSDK(s))
			if err != nil {
				return err
			}
			return s.Start()
		}, func(err error) {
			if s != nil {
				_ = s.Stop()
			}
		})
	}
	// Using SDK - gRPC events handlers
	{
		var s common.Service
		g.Add(func() (err error) {
			s, err = dapr_server_grpc.NewService(config.SDKGRPCAddr)
			if err != nil {
				return err
			}
			err = errors.Join(
				widgetRest.RegisterTopicEventHandlersSDK(s),
				gadgetRest.RegisterTopicEventHandlersSDK(s),
				productRest.RegisterTopicEventHandlersSDK(s))
			if err != nil {
				return err
			}
			return s.Start()
		}, func(err error) {
			if s != nil {
				_ = s.Stop()
			}
		})
	}
	// Termination signals
	g.Add(run.SignalHandler(ctx, os.Interrupt, os.Kill))

	var se run.SignalError
	if err := g.Run(); err != nil && !errors.As(err, &se) {
		log.Error(err, "goroutine error")
		_ = productRepo.Close()
		pool.Close()
		cancel()
		os.Exit(1)
	}
	_ = productRepo.Close()
	pool.Close()
	cancel()
}
