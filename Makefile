.PHONY: build run test test-coverage lint lint-fix openapi-validate openapi-compat docker-build docker-run clean help

BINARY_NAME=kubeorch-core
DOCKER_IMAGE=kubeorch/core
OPENAPI_SPEC?=docs/openapi.yaml
OPENAPI_BASE?=origin/main

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' 2>/dev/null || sed -n 's/^## //p' $(MAKEFILE_LIST)

## build: Build the binary
build:
	go build -o $(BINARY_NAME) .

## run: Run the server locally
run:
	go run .

## test: Run all tests with race detection
test:
	go test -v -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run golangci-lint
lint:
	golangci-lint run --timeout 5m

## lint-fix: Run golangci-lint with auto-fix
lint-fix:
	golangci-lint run --fix --timeout 5m

## openapi-validate: Validate the OpenAPI document and KubeOrch API conventions
openapi-validate:
	go -C tools/openapi run . validate ../../$(OPENAPI_SPEC)

## openapi-compat: Reject breaking OpenAPI changes compared with OPENAPI_BASE
openapi-compat: openapi-validate
	go -C tools/openapi run . breaking-ref $(OPENAPI_BASE) $(OPENAPI_SPEC) ../../$(OPENAPI_SPEC)

## docker-build: Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):latest .

## docker-run: Run Docker container
docker-run:
	docker run -p 3000:3000 --env-file .env $(DOCKER_IMAGE):latest

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html

## mod-tidy: Tidy Go modules
mod-tidy:
	go mod tidy

## vet: Run go vet
vet:
	go vet ./...
