# KubeOrchestra Core - Project Overview

## Project Vision
KubeOrchestra is a visual Kubernetes workflow orchestrator that transforms complex YAML configurations into intuitive drag-and-drop interfaces. The platform democratizes container orchestration by eliminating the steep learning curve of traditional Kubernetes tooling, making it accessible to developers of all skill levels.

## Core Architecture Philosophy
- **Zero-Configuration Default**: Everything works out-of-the-box with smart defaults
- **Auto-Discovery & Configuration**: Services automatically detect and configure connections
- **One-Click Plugins**: Add complete stacks (monitoring, logging, security) with single click
- **Frontend Simplicity**: UI sends only JSON with component IDs and configurations
- **Backend Intelligence**: Smart YAML generation with automatic dependency resolution
- **Template-Driven**: Predefined, tested YAML templates ensure reliability
- **Security First**: Centralized validation and transformation prevents malformed deployments
- **Progressive Complexity**: Show configuration only when absolutely necessary

## Core Value Propositions
- **Visual Workflow Design**: Drag-and-drop interface that generates simple JSON
- **Template System**: Curated library of production-ready Kubernetes templates
- **Intelligent YAML Generation**: Backend converts JSON to optimized Kubernetes manifests
- **End-to-End Integration**: Seamless flow from visual design to cluster deployment
- **Enterprise Ready**: Real-time cost optimization and automated compliance checking

## Current State

### What Exists
The backend (core) repository currently has:
- **Basic Go Web Server**: A minimal Gin-based web server running on port 3000
- **Project Structure**: Well-organized Go module with handlers, routes, middleware, and utilities
- **Configuration System**: Viper-based configuration with YAML and environment variable support
- **Logging**: Structured JSON logging with Logrus
- **CORS Support**: Basic CORS middleware configured
- **API Versioning**: Initial v1 API group structure

### Technology Stack
- **Language**: Go 1.25.0
- **Web Framework**: Gin (v1.10.1)
- **Configuration**: Viper
- **Logging**: Logrus
- **Environment**: Godotenv

### Current Limitations
- Only has a single "Hello World" endpoint (`GET /v1/`)
- No Kubernetes integration yet
- No authentication/authorization system
- No database connections
- No real business logic or orchestration features
- CORS allows all origins (security concern for production)

## Architecture Direction

### Template System Architecture
1. **Template Repository**: Store and manage predefined YAML templates
   - Deployment templates (web apps, databases, microservices)
   - Service templates (LoadBalancer, ClusterIP, NodePort)
   - ConfigMap and Secret templates
   - Ingress and networking templates
   - StatefulSet and DaemonSet templates

2. **JSON to YAML Transformation Engine**
   - Receive JSON workflow from frontend
   - Map component IDs to template references
   - Inject user parameters into templates
   - Generate complete Kubernetes manifests
   - Validate generated YAML against cluster capabilities

### Backend Services Needed
1. **Template Management Service**: CRUD operations for YAML templates
2. **Transformation Engine**: Convert JSON workflows to Kubernetes YAML
3. **Kubernetes Client Integration**: Apply generated YAML to clusters
4. **Validation Service**: Pre-flight checks before deployment
5. **Workflow Engine**: Handle complex multi-step deployments
6. **Policy Engine**: Apply security and compliance rules to generated YAML
7. **Resource Optimizer**: Inject optimal resource limits into templates

### API Design Philosophy
- **Simple JSON APIs**: Frontend sends only JSON payloads
- **Template-based**: All requests reference template IDs
- **Parameter Injection**: User inputs as simple key-value pairs
- **Async Operations**: Long-running deployments with status updates
- **WebSocket**: Real-time deployment progress and logs

## Next Phase Goals
1. Create comprehensive template library including Load Balancers, Istio, and advanced components
2. Build JSON-to-YAML transformation engine with automatic dependency resolution
3. Implement automatic image building with Nixpacks and GitHub integration
4. Create intelligent connection system with automatic port exposure
5. Build real-time container log streaming infrastructure
6. Implement smart configuration defaults with advanced override options
7. Establish automatic service discovery and dependency management

## Enhanced JSON Workflow Example
```json
{
  "workflowId": "e-commerce-app",
  "name": "E-Commerce Platform",
  "components": [
    {
      "id": "load-balancer",
      "templateId": "nginx-load-balancer",
      "parameters": {
        "type": "LoadBalancer",
        "algorithm": "round-robin",
        "healthCheck": {
          "path": "/health",
          "interval": 30
        }
      }
    },
    {
      "id": "frontend",
      "templateId": "web-app-deployment",
      "parameters": {
        "imageSource": {
          "type": "github",
          "repository": "myorg/frontend",
          "branch": "main",
          "buildpack": "nixpacks"
        },
        "replicas": 3,
        "configMode": "default"
      }
    },
    {
      "id": "backend-api",
      "templateId": "api-deployment",
      "parameters": {
        "image": "myapp/api:1.0",
        "replicas": 2,
        "advancedConfig": {
          "resources": {
            "cpu": "500m",
            "memory": "1Gi"
          },
          "autoscaling": {
            "min": 2,
            "max": 10,
            "cpuTarget": 70
          }
        }
      }
    },
    {
      "id": "postgres-db",
      "templateId": "postgres-statefulset",
      "parameters": {
        "version": "14",
        "storage": "10Gi"
      }
    },
    {
      "id": "service-mesh",
      "templateId": "istio-gateway",
      "parameters": {
        "trafficPolicy": "round-robin",
        "mtls": true,
        "retries": 3
      }
    }
  ],
  "connections": [
    {
      "from": "load-balancer",
      "to": "frontend",
      "type": "http",
      "port": "auto"
    },
    {
      "from": "frontend",
      "to": "backend-api",
      "type": "http",
      "exposedPorts": "auto"
    },
    {
      "from": "backend-api",
      "to": "postgres-db",
      "type": "database",
      "protocol": "postgresql"
    }
  ]
}
```

The backend transforms this simple JSON into complete Kubernetes manifests with proper networking, services, and configurations.

## Technical Debt & Improvements Needed
- Restrict CORS to specific origins
- Add comprehensive error handling
- Implement request validation middleware
- Add metrics and monitoring (Prometheus)
- Set up OpenTelemetry for distributed tracing
- Create API documentation (OpenAPI/Swagger)
- Add comprehensive testing framework
- Implement CI/CD pipelines

## Integration Points
- **Frontend**: React-based UI (separate repository)
- **Kubernetes Clusters**: Multiple cluster support
- **Cloud Providers**: AWS, GCP, Azure
- **Git Providers**: GitHub, GitLab, Bitbucket
- **Container Registries**: Docker Hub, ECR, GCR, ACR
- **Monitoring**: Prometheus, Grafana
- **CNCF Tools**: Helm, Flux, ArgoCD, Tekton