# KubeOrchestra Core Makefile

# Variables
APP_NAME=kubeorchestra-core
BINARY_NAME=core
GO_VERSION=1.25.0
DOCKER_IMAGE=$(APP_NAME)
DOCKER_TAG=latest

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Build the application
.PHONY: build
build:
	@echo "Building $(APP_NAME)..."
	$(GOBUILD) -o bin/$(BINARY_NAME) -v .

# Run the application
.PHONY: run
run: db-check
	@echo "Running $(APP_NAME)..."
	$(GOBUILD) -o bin/$(BINARY_NAME) -v .
	./bin/$(BINARY_NAME)

# Run without building (go run)
.PHONY: dev
dev: db-check
	@echo "Running $(APP_NAME) in development mode..."
	$(GOCMD) run .
	
# Run app with migration
.PHONY: dev-migrate
dev-migrate: db-check
	@echo "Running migrations and starting app..."
	$(GOCMD) run . --migrate

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f bin/$(BINARY_NAME)
	rm -rf bin/

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code (requires golangci-lint)
.PHONY: lint
lint:
	@echo "Linting code..."
	golangci-lint run

# Check if database is running and start if needed
.PHONY: db-check
db-check:
	@if docker compose ps postgres | grep -q "Up"; then \
		echo "Database is already running"; \
	else \
		echo "Database is not running, starting..."; \
		make db-up; \
	fi

# Start PostgreSQL database
.PHONY: db-up
db-up:
	@echo "Starting PostgreSQL database..."
	@docker compose up -d postgres
	@echo "Checking if database is ready..."
	@timeout 30 bash -c 'until docker compose exec -T postgres pg_isready -U kubeorch_user -d kubeorch_db; do sleep 1; done' || echo "Database ready check timed out, continuing anyway..."

# Stop PostgreSQL database
.PHONY: db-down
db-down:
	@echo "Stopping PostgreSQL database..."
	docker compose down

# Restart PostgreSQL database
.PHONY: db-restart
db-restart:
	@echo "Restarting PostgreSQL database..."
	docker compose down postgres
	docker compose up -d postgres
	@echo "Waiting for database to be ready..."
	@timeout 30 bash -c 'until docker compose exec -T postgres pg_isready -U kubeorch_user -d kubeorch_db; do sleep 1; done' || echo "Database ready check timed out, continuing anyway..."

# Start all services
.PHONY: up
up:
	@echo "Starting all services..."
	docker compose up -d

# Stop all services
.PHONY: down
down:
	@echo "Stopping all services..."
	docker compose down

# View database logs
.PHONY: db-logs
db-logs:
	@echo "Viewing PostgreSQL logs..."
	docker compose logs -f postgres

# Connect to PostgreSQL database
.PHONY: db-connect
db-connect:
	@echo "Connecting to PostgreSQL database..."
	docker compose exec postgres psql -U kubeorch_user -d kubeorch_db

# Build for production (optimized)
.PHONY: build-prod
build-prod:
	@echo "Building for production..."
	CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -ldflags '-extldflags "-static"' -o bin/$(BINARY_NAME) .

# Build Docker image
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Run with hot reload (requires air)
.PHONY: watch
watch: db-check
	@echo "Starting with hot reload..."
	air

# Install development tools
.PHONY: install-tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/air-verse/air@latest

# Show help
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  build         - Build the application"
	@echo "  run           - Smart start database and run the application"
	@echo "  dev           - Smart start database and run in development mode"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  deps          - Download dependencies"
	@echo "  tidy          - Tidy dependencies"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  db-check      - Check if database is running, start if needed"
	@echo "  db-start      - Start database if not running"
	@echo "  db-up         - Force start PostgreSQL database"
	@echo "  db-restart    - Restart PostgreSQL database"
	@echo "  db-down       - Stop PostgreSQL database"
	@echo "  up            - Start all services"
	@echo "  down          - Stop all services"
	@echo "  db-logs       - View database logs"
	@echo "  db-connect    - Connect to database"
	@echo "  build-prod    - Build for production"
	@echo "  docker-build  - Build Docker image"
	@echo "  watch         - Smart start database and run with hot reload"
	@echo "  install-tools - Install development tools"
	@echo "  help          - Show this help message"

# Default target
.DEFAULT_GOAL := help