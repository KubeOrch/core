# KubeOrch Core - Makefile
# For development environment, use orchcli from the CLI repo:
#   orchcli dev start    - Start full development environment
#   orchcli dev logs     - View logs
#   orchcli dev stop     - Stop environment

# Variables
BINARY_NAME=core
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Build the application
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@$(GOBUILD) -o bin/$(BINARY_NAME) -v .

# Run the application locally (requires database)
.PHONY: run
run:
	@echo "Running $(BINARY_NAME)..."
	@echo "Note: Database must be running. Use 'orchcli dev start' for full environment."
	@$(GOCMD) run .

# Run with migration flag
.PHONY: run-migrate
run-migrate:
	@echo "Running with migrations..."
	@$(GOCMD) run . --migrate

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	@$(GOTEST) -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@$(GOTEST) -v -coverprofile=coverage.out ./...
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@$(GOMOD) download

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	@$(GOMOD) tidy

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@$(GOFMT) -s -w .

# Lint code (requires golangci-lint)
.PHONY: lint
lint:
	@echo "Linting code..."
	@$(GOLINT) run

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	@$(GOCLEAN)
	@rm -rf bin/ coverage.out coverage.html

# Build for production (static binary)
.PHONY: build-prod
build-prod:
	@echo "Building for production..."
	@CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -ldflags '-extldflags "-static"' -o bin/$(BINARY_NAME) .

# Build Docker image
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	@docker build -t ghcr.io/kubeorch/core:latest .

# Install development tools
.PHONY: install-tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/air-verse/air@latest
	@echo "Tools installed!"

# Watch mode with hot reload (requires air)
.PHONY: watch
watch:
	@echo "Starting with hot reload..."
	@echo "Note: Database must be running. Use 'orchcli dev start' for full environment."
	@air

# Show help
.PHONY: help
help:
	@echo "KubeOrch Core - Available Commands"
	@echo ""
	@echo "DEVELOPMENT ENVIRONMENT:"
	@echo "  Use orchcli from the CLI repository for full environment:"
	@echo "    orchcli dev start    - Start all services (UI, Core, DB)"
	@echo "    orchcli dev logs     - View logs"
	@echo "    orchcli dev stop     - Stop all services"
	@echo ""
	@echo "GO-SPECIFIC TASKS:"
	@echo "  make build          - Build the binary"
	@echo "  make run            - Run the application (requires DB)"
	@echo "  make run-migrate    - Run with database migrations"
	@echo "  make test           - Run tests"
	@echo "  make test-coverage  - Run tests with coverage report"
	@echo "  make watch          - Run with hot reload (requires air)"
	@echo ""
	@echo "CODE QUALITY:"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Lint code (requires golangci-lint)"
	@echo "  make tidy           - Tidy go.mod dependencies"
	@echo ""
	@echo "BUILD & DEPLOY:"
	@echo "  make build-prod     - Build static binary for production"
	@echo "  make docker-build   - Build Docker image"
	@echo ""
	@echo "UTILITIES:"
	@echo "  make deps           - Download dependencies"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make install-tools  - Install development tools"

# Default target
.DEFAULT_GOAL := help