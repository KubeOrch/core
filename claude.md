# KubeOrch Core

Backend API server for KubeOrch - a Kubernetes orchestration and workflow management platform.

## Tech Stack

- **Language**: Go 1.25
- **Web Framework**: Gin
- **Database**: MongoDB
- **Kubernetes**: client-go for cluster operations
- **Auth**: JWT with access/refresh tokens
- **Logging**: Logrus (JSON format)
- **Config**: Viper

## Project Structure

```
main.go                 # Application entrypoint

handlers/               # HTTP request handlers
├── auth_handler.go     # Login, register, token refresh
├── cluster_handler.go  # Cluster CRUD, status, sharing
├── resources_handler.go # K8s resource operations, logs, terminal
├── workflow_handler.go # Workflow CRUD, execution
└── settings_handler.go # User/admin settings

services/               # Business logic
├── auth_service.go
├── user_service.go
├── kubernetes_cluster_service.go
├── resource_service.go
├── workflow_service.go
├── workflow_executor.go
├── cluster_health_monitor.go    # Background health checks (60s)
└── resource_sync_monitor.go     # Background resource sync (5min)

models/                 # Data structures
├── auth.go
├── user.go
├── cluster.go
├── resource.go
└── workflow.go

repositories/           # Database access layer
routes/                 # Route definitions
├── routes.go           # All API routes

middleware/             # HTTP middleware
├── auth.go             # JWT validation
├── admin.go            # Admin role check
└── logs.go             # Request logging

database/               # MongoDB connection
pkg/                    # Shared packages
utils/                  # Utilities (config, etc.)
templates/              # Workflow templates
```

## API Routes

All routes are prefixed with `/v1`:

### Auth (Public)
- `POST /v1/api/auth/register` - User registration
- `POST /v1/api/auth/login` - Login, returns JWT
- `POST /v1/api/auth/refresh` - Refresh access token

### Protected Routes (Require JWT)

**Profile**
- `GET /v1/api/profile` - Get current user
- `PUT /v1/api/profile` - Update profile

**Clusters**
- `POST /v1/api/clusters` - Add cluster
- `GET /v1/api/clusters` - List clusters
- `GET /v1/api/clusters/:name` - Get cluster
- `PUT /v1/api/clusters/:name` - Update cluster
- `DELETE /v1/api/clusters/:name` - Remove cluster
- `GET /v1/api/clusters/:name/status` - Health status
- `POST /v1/api/clusters/:name/test` - Test connection
- `GET /v1/api/clusters/:name/logs` - Cluster logs

**Resources**
- `GET /v1/api/resources` - List K8s resources
- `POST /v1/api/resources/sync` - Sync from cluster
- `GET /v1/api/resources/:id` - Get resource
- `PATCH /v1/api/resources/:id` - Update user fields
- `GET /v1/api/resources/:id/logs/stream` - Stream pod logs (SSE)
- `GET /v1/api/resources/:id/exec/terminal` - Terminal session (WebSocket)

**Workflows**
- `POST /v1/api/workflows` - Create workflow
- `GET /v1/api/workflows` - List workflows
- `GET /v1/api/workflows/:id` - Get workflow
- `PUT /v1/api/workflows/:id` - Update workflow
- `DELETE /v1/api/workflows/:id` - Delete workflow
- `POST /v1/api/workflows/:id/run` - Execute workflow
- `GET /v1/api/workflows/:id/runs` - Get execution history

## Development

```bash
# Build the binary
go build -o kubeorch

# Run the server
./kubeorch

# Or run directly
go run main.go
```

## Configuration

Configuration via `config.yaml` or environment variables:

```yaml
server:
  port: "8080"
  gin_mode: "debug"  # debug, release, test
  log_level: "info"

database:
  mongodb_uri: "mongodb://localhost:27017"
  database_name: "kubeorch"

jwt:
  secret: "your-secret-key"
  expiry_hours: 24
```

## Background Services

- **ClusterHealthMonitor**: Checks cluster connectivity every 60 seconds
- **ResourceSyncMonitor**: Syncs K8s resources every 5 minutes

## Frontend Integration

This backend serves the **ui** frontend:
- CORS enabled for all origins (configure in production)
- JWT tokens in Authorization header: `Bearer <token>`
- SSE for log streaming, WebSocket for terminal
- First registered user becomes admin

## Code Style

- Standard Go project layout
- Handlers call services, services call repositories
- Use Logrus for all logging
- Return proper HTTP status codes
- Validate input in handlers
