.DEFAULT_GOAL := help

APP_NAME       := dapr-go-hero
CURRENTTAG     := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
NEWTAG         ?= $(shell bash -c 'read -p "Please provide a new tag (current tag - $(CURRENTTAG)): " newtag; echo $$newtag')
GOFLAGS        := -mod=mod

# === Tool Versions (pinned) ===
GOSEC_VERSION         := v2.25.0
GOCRITIC_VERSION      := v0.14.3
GOLANGCI_LINT_VERSION := v2.11.1

#help: @ List available tasks
help:
	@echo "Usage: make COMMAND"
	@echo "Commands :"
	@grep -E '[a-zA-Z\.\-]+:.*?@ .*$$' $(MAKEFILE_LIST)| tr -d '#' | awk 'BEGIN {FS = ":.*?@ "}; {printf "\033[32m%-25s\033[0m - %s\n", $$1, $$2}'

#deps: @ Install required tool dependencies (idempotent)
deps:
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec $(GOSEC_VERSION)..."; go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION); }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); }
	@command -v gocritic >/dev/null 2>&1 || { echo "Installing gocritic $(GOCRITIC_VERSION)..."; go install -v github.com/go-critic/go-critic/cmd/gocritic@$(GOCRITIC_VERSION); }

#clean: @ Remove build artifacts
clean:
	@rm -f ./cmd/inventory/main ./cmd/products/products
	@echo "Build artifacts removed."

#lint: @ Run golangci-lint
lint: deps
	@golangci-lint run ./...

#critic: @ Run gocritic with all checks enabled
critic: deps
	@gocritic check -enableAll ./...

#sec: @ Run gosec security scanner
sec: deps
	@gosec -exclude-dir=proto ./...

#test: @ Run tests
test:
	@export GOFLAGS=$(GOFLAGS); go test -v ./...

#build: @ Build inventory and products binaries
build: deps lint critic sec
	@export GOFLAGS=$(GOFLAGS); export CGO_ENABLED=0; export GOOS=linux; export GOARCH=amd64; go build -a -o ./cmd/inventory/main ./cmd/inventory/main.go
	@export GOFLAGS=$(GOFLAGS); export CGO_ENABLED=0; export GOOS=linux; export GOARCH=amd64; go build -a -o ./cmd/products/products ./cmd/products/main.go

#run: @ Run inventory service with Dapr (SDK HTTP mode)
run:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#update: @ Update dependencies to latest versions
update:
	@export GOFLAGS=$(GOFLAGS); go get -u ./...; go mod tidy

#ci: @ Run full local CI pipeline
ci: deps lint critic sec test build
	@echo "Local CI pipeline passed."

#release: @ Create and push a new tag
release: build
	@bash -c 'newtag="$(NEWTAG)" && \
		echo "$$newtag" | grep -qE "^v[0-9]+\.[0-9]+\.[0-9]+$$" || { echo "Error: Tag must match vN.N.N (got: $$newtag)"; exit 1; } && \
		echo -n "Are you sure to create and push $$newtag tag? [y/N] " && read ans && [ "$${ans:-N}" = y ] && \
		git add -A && \
		git commit -a -s -m "Cut $$newtag release" && \
		git tag $$newtag && \
		git push origin $$newtag && \
		git push && \
		echo "Done."'

#run-test: @ Run inventory with Dapr sidecar only (no app)
run-test:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- sleep 6000

#run-custom-http: @ Run inventory with custom HTTP client
run-custom-http:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- go run cmd/inventory/main.go http

#run-custom-grpc: @ Run inventory with custom gRPC client
run-custom-grpc:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4001 --dapr-http-port 3500 -- go run cmd/inventory/main.go grpc

#run-sdk-http: @ Run inventory with Go SDK HTTP client
run-sdk-http:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-sdk-grpc: @ Run inventory with Go SDK gRPC client
run-sdk-grpc:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-products: @ Run Products gRPC service
run-products:
	@dapr run --app-id products --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 50151 -- go run cmd/products/main.go

#send-widget: @ Publish widget event to Dapr PubSub
send-widget:
	@cat messages/widget.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/widget.json

#send-gadget: @ Publish gadget event to Dapr PubSub
send-gadget:
	@cat messages/gadget.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/gadget.json

#send-thingamajig: @ Publish thingamajig event to Dapr PubSub
send-thingamajig:
	@cat messages/thingamajig.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/thingamajig.json

#send-all: @ Publish all three event types
send-all: send-widget send-gadget send-thingamajig

#get-widget: @ Fetch widget from REST API
get-widget:
	@curl -s http://localhost:3000/v1/widgets/widget | jq

#get-gadget: @ Fetch gadget from REST API
get-gadget:
	@curl -s http://localhost:3000/v1/gadgets/gadget | jq

#get-thingamajig: @ Fetch product from REST API
get-thingamajig:
	@curl -s http://localhost:3000/v1/products/thingamajig | jq

#get-all: @ Fetch all three items from REST API
get-all: get-widget get-gadget get-thingamajig

.PHONY: help deps clean lint critic sec test build run update ci release \
	run-test run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc run-products \
	send-widget send-gadget send-thingamajig send-all \
	get-widget get-gadget get-thingamajig get-all
