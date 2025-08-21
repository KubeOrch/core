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
```

## API

The server provides a simple API endpoint:

- `GET /v1/` - Returns a greeting message

Example:
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

## Contributing

We welcome contributions! Please see our contributing guidelines for more details.

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.

