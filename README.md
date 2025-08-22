# KubeOrchestra Core

[![Apache 2.0 License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Cloud Native](https://img.shields.io/badge/Cloud%20Native-orange.svg)](https://landscape.cncf.io/)

A simple and lightweight Go web server for the KubeOrchestra project.

## About

KubeOrchestra Core is the main orchestration engine and API gateway for the KubeOrchestra platform. It provides a clean, simple API interface built with modern Go practices.

## Quick Start

### Prerequisites
- Go 1.25.0 or higher

### Installation

```bash
# Clone the repository
git clone https://github.com/KubeOrchestra/core.git
cd core

# Install dependencies
go mod tidy

# Run the application
go run main.go
```

### Configuration

Create a `config.yaml` file in the project root:

```yaml
# Server port
PORT: 3000

# Gin mode: debug, release, test
GIN_MODE: debug
```

Or use environment variables:
```bash
export PORT=3000
export GIN_MODE=release
export JWT_SECRET=your-super-secret-jwt-key-here
```

**Note:** For authentication features, you must set the `JWT_SECRET` environment variable.

## API

The server provides the following API endpoints:

### Core Endpoints
- `GET /v1/` - Returns a greeting message

### Authentication Endpoints
- `POST /api/auth/register` - User registration
- `POST /api/auth/login` - User authentication

#### Authentication Examples

**Register a new user:**
```bash
curl -X POST http://localhost:3000/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123",
    "name": "John Doe"
  }'
```

**Login with credentials:**
```bash
curl -X POST http://localhost:3000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123"
  }'
```

**Test basic endpoint:**
```bash
curl http://localhost:3000/v1/
```

## Development

### Building
```bash
go build -o kubeorchestra main.go
```

### Running
```bash
./kubeorchestra
```

### Features

- **JWT Authentication**: Secure user registration and login with JWT tokens
- **Password Hashing**: bcrypt password hashing with cost factor 12
- **Thread-Safe**: Concurrent-safe user storage with mutex protection
- **Input Validation**: Email validation and password requirements
- **Token Expiration**: 24-hour JWT token expiration
- **Middleware**: JWT token validation middleware for protected routes
- **Storage**: Currently using in-memory storage (data persists during server runtime)

## Contributing

We welcome contributions! Please see our contributing guidelines for more details.

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.

