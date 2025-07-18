PHONY: run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc
PHONY: send-widget send-gadget send-thingamajig send-all
PHONY: get-widget get-gadget get-thingamajig get-all

run-test:
	dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- sleep 6000

run-custom-http:
	dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- go run cmd/inventory/main.go http

run-custom-grpc:
	dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4001 --dapr-http-port 3500 -- go run cmd/inventory/main.go grpc

run-sdk-http:
	dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

run-sdk-grpc:
	dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

run-products:
	dapr run --app-id products --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 50151 -- go run cmd/products/main.go

send-widget:
	cat messages/widget.json | jq
	curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/widget.json

send-gadget:
	cat messages/gadget.json | jq
	curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/gadget.json

send-thingamajig:
	cat messages/thingamajig.json | jq
	curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/thingamajig.json

send-all: send-widget send-gadget send-thingamajig

get-widget:
	curl -s http://localhost:3000/v1/widgets/widget | jq

get-gadget:
	curl -s http://localhost:3000/v1/gadgets/gadget | jq

get-thingamajig:
	curl -s http://localhost:3000/v1/products/thingamajig | jq

get-all: get-widget get-gadget get-thingamajig

test:
	@go test ./...

build:
	@export CGO_ENABLED=0; GOOS=linux GOARCH=amd64 go build -o ./cmd/inventory/main ./cmd/inventory/main.go

update:
	@go get -u ./...; go mod tidy