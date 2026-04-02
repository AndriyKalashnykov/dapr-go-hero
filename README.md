[![CI](https://github.com/AndriyKalashnykov/dapr-go-hero/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/AndriyKalashnykov/dapr-go-hero/actions/workflows/ci.yml)
[![Hits](https://hits.sh/github.com/AndriyKalashnykov/dapr-go-hero.svg?view=today-total&style=plastic)](https://hits.sh/github.com/AndriyKalashnykov/dapr-go-hero/)
[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Renovate enabled](https://img.shields.io/badge/renovate-enabled-brightgreen.svg)](https://app.renovatebot.com/dashboard#github/AndriyKalashnykov/dapr-go-hero)

# From Zero to Hero with Go and Dapr

[Slides](slides.pdf)

This is a Go application demonstrating the key features of [Dapr](https://dapr.io) with a few different approaches. Built with Fiber v3, pgx v5, gRPC/protobuf, Redis, and PostgreSQL. My goal is to help you pick the best fit for your needs and level up as a microservices developer.

Dapr, at its core, is a set of building block APIs that abstract away common tasks so you can focus on what matters most-- business value/features. We will focus on the building blocks that I consider as the most useful for Go developers.

* Publish / Subscribe (with routing)
* Secret store
* State store
* Service Invocation (with discovery and tracing)

## Quick Start

```bash
make deps               # install tool dependencies (gosec, golangci-lint)
make build              # compile inventory and products binaries
make test               # run tests
make run-products       # start Products gRPC service (terminal 1)
make run                # start Inventory service with Dapr (terminal 2)
```

> **Note:** Running the services requires `dapr init`, a PostgreSQL container, and the `widgets` table from `tables.sql`. See [Running the demo](#running-the-demo) for full setup instructions.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| [Go](https://go.dev/dl/) | 1.26+ | Language runtime and compiler |
| [GNU Make](https://www.gnu.org/software/make/) | 3.81+ | Build orchestration |
| [Dapr CLI](https://docs.dapr.io/getting-started/install-dapr-cli/) | latest | Microservices runtime (`dapr init` provides Redis) |
| [Docker](https://www.docker.com/) | latest | PostgreSQL and Redis containers |
| [jq](https://jqlang.github.io/jq/) | latest | Pretty-printing JSON responses |
| [protoc](https://grpc.io/docs/protoc-installation/) | latest | Regenerating protobuf code (optional) |
| [act](https://github.com/nektos/act) | latest | Running GitHub Actions locally (optional) |

Install all required tool dependencies:

```bash
make deps
```

## Available Make Targets

Run `make help` to see all available targets.

### Build & Test

| Target | Description |
|--------|-------------|
| `make build` | Build inventory and products binaries |
| `make test` | Run tests |
| `make clean` | Remove build artifacts |
| `make update` | Update dependencies to latest versions |

### Code Quality

| Target | Description |
|--------|-------------|
| `make lint` | Run golangci-lint (includes gocritic via .golangci.yml) |
| `make sec` | Run gosec security scanner |

### CI

| Target | Description |
|--------|-------------|
| `make ci` | Run full local CI pipeline |
| `make ci-run` | Run GitHub Actions workflow locally via [act](https://github.com/nektos/act) |

### Dapr Services

| Target | Description |
|--------|-------------|
| `make run` | Run inventory service with Dapr (SDK HTTP mode) |
| `make run-products` | Run Products gRPC service |
| `make run-custom-http` | Run inventory with custom HTTP client |
| `make run-custom-grpc` | Run inventory with custom gRPC client |
| `make run-sdk-http` | Run inventory with Go SDK HTTP client |
| `make run-sdk-grpc` | Run inventory with Go SDK gRPC client |
| `make dapr-test` | Run inventory with Dapr sidecar only (no app) |

### Publish Events

| Target | Description |
|--------|-------------|
| `make send-widget` | Publish widget event to Dapr PubSub |
| `make send-gadget` | Publish gadget event to Dapr PubSub |
| `make send-thingamajig` | Publish thingamajig event to Dapr PubSub |
| `make send-all` | Publish all three event types |

### Query REST API

| Target | Description |
|--------|-------------|
| `make get-widget` | Fetch widget from REST API |
| `make get-gadget` | Fetch gadget from REST API |
| `make get-thingamajig` | Fetch product from REST API |
| `make get-all` | Fetch all three items from REST API |

### Utilities

| Target | Description |
|--------|-------------|
| `make help` | List available tasks |
| `make deps` | Install required tool dependencies (idempotent) |
| `make deps-act` | Install act for running GitHub Actions locally |
| `make deps-check` | Show required Go versions and gvm status |
| `make deps-prune` | Remove unused and redundant dependencies |
| `make deps-prune-check` | Verify no prunable dependencies (CI gate) |
| `make deps-renovate` | Install nvm and npm for Renovate |
| `make release` | Create and push a new tag |
| `make renovate-validate` | Validate Renovate configuration |

## Design choices

When building Go microservices, we have many choices to make! gRPC, REST or both? Which HTTP router? How to organize packages?

In this application, packages are organized by purpose/feature. This creates a small hurdle for subscriptions because your application [responds with all of the topics in a single callback](https://docs.dapr.io/developing-applications/building-blocks/pubsub/howto-publish-subscribe/#step-2-subscribe-to-topics). To work around this, the subscriptions from each package are merged together into a single response.

You will find examples of "helper code" like this in `pkg/dapr`. However, be aware that the [Go SDK](https://github.com/dapr/go-sdk) is an abstraction over all of the Dapr APIs. It is up to you to decide on custom code or the SDK.

The Go SDK uses the [standard net/http package](https://pkg.go.dev/net/http). To be different, I choose [Fiber](https://gofiber.io/) as the HTTP router for public API traffic and found it to be straightforward to use.

Event callbacks, such as PubSub events, should be considered private communication. Because of this, I highly recommend having Dapr callbacks listen on a separate port that is not publicly exposed. The gRPC callback listener binds to the loopback interface (`127.0.0.1`) only for this reason.

## Application flow

With the design decisions out of the way, let's look at the scenarios and specifically how the Dapr building blocks are used.

There are three code paths. Each starts with the receipt of a Pub/Sub event. By default, Dapr uses [CloudEvents](https://cloudevents.io/) envelopes to encapsulate event data. This allows Dapr to attach tracing IDs and [route the event](https://docs.dapr.io/developing-applications/building-blocks/pubsub/howto-route-messages/) based on attributes such as type.

The application is an Inventory service, where products are persisted in different stores based on type. `Widgets` are stored in a PostgreSQL database. `Gadgets` are stored in a State store, such as Redis or MongoDB. All other product types are stored in a separate service exposed through gRPC.

![Demo diagram](demo.png)

By inspecting `event.type`, Dapr selects one of three routes (URIs) to invoke.

To connect to the PostgreSQL database, the application uses a Secret Store to acquire the needed credentials in order to connect. From there, the [jackc/pgx](https://github.com/jackc/pgx) package is used to execute SQL statements.

Gadgets are saved simply by calling the "Save state" operation of the [State management API](https://docs.dapr.io/reference/api/state_api/).

Finally, general products are stored in the Products gRPC service. The developer uses the generated gRPC client as normal; however, the endpoint is the Dapr sidecar and an additional `dapr-app-id` metadata field is attached to the request so Dapr know how to route the request. See this [How-To](https://docs.dapr.io/developing-applications/building-blocks/service-invocation/howto-invoke-services-grpc/) for more details.

All component configurations are located in the `components` directory. The main Dapr configuration is in `config.yaml` and is where tracing and preview features are enabled.

## Running the demo

After running `dapr init`, you should have Redis running in a Docker container. You will need to create a PostgreSQL database and update `secrets.json` accordingly. Then create the `widgets` table from `tables.sql`.

```shell
docker run --name postgres -e POSTGRES_PASSWORD=postgres -p 5432:5432 -d postgres
cat tables.sql | docker exec -i postgres psql -U postgres -d postgres
```

**Start the Products service**

```shell
make run-products
```

**Start the main Inventory service**

In a second terminal run: (_Pick the client mode_)

```shell
make run-custom-http
make run-custom-grpc
# or
make run-sdk-http
make run-sdk-grpc
```

**Send product events**

In a third terminal you can publish the 3 product event types. The contents of each message are located in the `messages` directory.

Send a Widget: This will save in the PostgreSQL database.

```shell
make send-widget
```

Send a Gadget: This will save in the Redis state store.

```shell
make send-gadget
```

Send a Thingamajig: This will invoke the Products service using Dapr for service discovery and mTLS authentication.

```shell
make send-thingamajig
```

**Query the REST API**

```shell
make get-widget
make get-gadget
make get-thingamajig
# or all at once
make get-all
```

That's it!

I hope this was helpful! If you have better ways of handling anything in this sample, please submit a PR! :)

## CI/CD

GitHub Actions runs on every push to `main`, tags `v*`, and pull requests.

| Job | Depends on | Steps |
|-----|-----------|-------|
| **static-check** | — | Lint, Security |
| **build** | static-check | Build |
| **test** | static-check | Test |

[Renovate](https://docs.renovatebot.com/) keeps dependencies up to date with platform automerge enabled.

### References

* [From Zero to Hero with Go and Dapr](https://github.com/pkedy/golang-dapr)
* [Code - Building Cloud-Native Services with Dapr, Go, and Kubernetes](https://github.com/vladimirvivien/dapr-examples)
* [Article - Building Cloud-Native Services with Dapr, Go, and Kubernetes](https://medium.com/@vladimirvivien/building-cloud-native-services-with-dapr-go-and-kubernetes-part-2-f773d484ecb0)
