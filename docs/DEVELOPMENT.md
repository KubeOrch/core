# Development Guide

## Quick Start

KubeOrchestra uses `orchcli` for development environment orchestration. The docker-compose and orchestration files have been moved to the CLI repository for centralized management.

### Prerequisites
1. Install orchcli: `npm install -g @kubeorchestra/orchcli`
2. Docker and Docker Compose installed
3. Go 1.21+ installed (for local development)

### Development Workflow

#### Using orchcli (Recommended)
```bash
# From the parent directory containing all repos
orchcli init         # Clone UI and Core repositories
orchcli dev start    # Start full environment (UI, Core, PostgreSQL)
orchcli dev logs     # View logs
orchcli dev stop     # Stop environment
```

#### Local Go Development
If you want to run the Core service locally while using orchcli for other services:

```bash
# Start only database and UI
orchcli dev start --core-only

# In the core directory
make run            # Run the application
# or
make watch          # Run with hot reload (requires air)
```

### Available Make Commands

#### Development
- `make run` - Run the application (database must be running)
- `make run-migrate` - Run with database migrations
- `make watch` - Run with hot reload using air
- `make test` - Run tests
- `make test-coverage` - Generate coverage report

#### Code Quality
- `make fmt` - Format code
- `make lint` - Run linter
- `make tidy` - Clean up go.mod

#### Build
- `make build` - Build binary
- `make build-prod` - Build static production binary
- `make docker-build` - Build Docker image

### Project Structure

```
core/
├── api/            # API handlers
├── cmd/            # CLI commands
├── database/       # Database connection and migrations
├── handlers/       # HTTP handlers
├── middleware/     # HTTP middleware
├── model/          # Data models
├── routes/         # Route definitions
├── utils/          # Utilities
├── bin/            # Compiled binaries (git ignored)
├── config.yaml     # Configuration file
├── go.mod          # Go dependencies
└── main.go         # Entry point
```

### Environment Variables

The application uses the following environment variables:

```bash
# Server
PORT=3000
GIN_MODE=debug|release

# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=kubeorch_user
DB_PASSWORD=kubeorch_password
DB_NAME=kubeorch_db
```

### Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package tests
go test ./handlers/...
```

### Database Migrations

The application handles migrations automatically on startup when using the `--migrate` flag:

```bash
make run-migrate
```

### Docker Development

While docker-compose files are now in the CLI repo, you can still build the Docker image:

```bash
make docker-build
```

## Why This Setup?

1. **Centralized Orchestration**: All docker-compose and development setup lives in the CLI repo
2. **Clean Separation**: Core repo focuses only on Go application code
3. **Better Developer Experience**: Single tool (orchcli) manages everything
4. **Easier Maintenance**: Update orchestration without modifying application repos

## Need Help?

- Check the CLI repository for orchestration details
- Run `make help` for available commands
- Run `orchcli --help` for CLI commands