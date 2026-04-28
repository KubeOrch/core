# KubeOrch Core

[![Apache 2.0 License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![CNCF Aspiring](https://img.shields.io/badge/CNCF-Aspiring-blue.svg)](https://www.cncf.io/projects/)

Backend service for KubeOrch - Transform Kubernetes complexity into simple drag-and-drop workflows.

## 🎯 Vision

KubeOrch democratizes Kubernetes by eliminating YAML complexity. Just drag, drop, and deploy - everything auto-configures intelligently.

## 🚀 What is KubeOrch Core?

The intelligent backend that powers the visual orchestration platform:

- **Zero-Configuration Engine** - Smart defaults for everything, no YAML needed
- **Template Library** - 150+ pre-configured services (databases, queues, ML platforms)
- **Auto-Wiring Magic** - Services automatically discover and connect to each other
- **One-Click Plugins** - Deploy complete stacks (monitoring, logging, security) instantly
- **Intelligent Dependencies** - Automatic port management and service discovery

## ✨ Key Features

- 🔄 **JSON to YAML Transformation** - Convert visual workflows to production-ready Kubernetes manifests
- 🔌 **Automatic Connection Resolution** - Services find their dependencies automatically
- 📦 **Nixpacks Integration** - Build containers from GitHub repos automatically
- 🎯 **Service Mesh Support** - Built-in Istio, load balancers, and ingress
- 📊 **Real-time Streaming** - Live logs and metrics via WebSocket
- 🔒 **Security First** - Automatic TLS, network policies, and RBAC

## 🛠️ Tech Stack

- **Language**: Go 1.25.0+
- **Framework**: Gin
- **Database**: MongoDB
- **Kubernetes**: client-go
- **Container Build**: Nixpacks
- **Real-time**: WebSocket + SSE

## 🚦 Quick Start

```bash
# Clone the repository
git clone https://github.com/KubeOrch/core.git
cd core

# Install dependencies
go mod tidy

# Run the server
go run main.go
```

Server starts at `http://localhost:3000`

## 📁 Project Structure

```
core/
├── handlers/       # API request handlers
├── templates/      # Service templates (PostgreSQL, Redis, etc.)
├── middleware/     # HTTP middleware
├── routes/         # API routes
└── utils/          # Utilities and helpers
```

## 🔗 API Overview

- `POST /v1/workflows/deploy` - Deploy visual workflow
- `GET /v1/templates` - Get available service templates
- `GET /v1/plugins` - List one-click plugins
- `WS /v1/logs` - Stream container logs
- `POST /v1/connections/auto` - Auto-wire services

## 🤝 Contributing

We welcome contributions! See the [contributing guide](https://github.com/KubeOrch/.github/blob/main/CONTRIBUTING.md).

## 📄 License

Apache License 2.0 - see [LICENSE](LICENSE) file for details.