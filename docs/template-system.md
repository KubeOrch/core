# KubeOrchestra Template System Architecture

## Overview

KubeOrchestra Core uses a hybrid approach combining **Helm** for templating and **Kustomize** for environment-specific customizations. This provides maximum flexibility while maintaining simplicity.

## Why Helm + Kustomize?

### Helm Use Cases in KubeOrchestra:
1. **Template Engine**: Generate Kubernetes manifests from JSON input
2. **Package Management**: Bundle related resources together
3. **Parameterization**: Inject user values into templates
4. **Dependency Management**: Handle inter-service dependencies
5. **Versioning**: Track template versions for rollbacks

### Kustomize Use Cases in KubeOrchestra:
1. **Environment Overlays**: Dev/Staging/Prod variations
2. **Last-Mile Customization**: User-specific tweaks without forking
3. **Resource Patching**: Modify generated YAML without templates
4. **Label/Annotation Management**: Consistent metadata across resources
5. **Secret/ConfigMap Generation**: From files or literals

## Architecture

```
core/
├── templates/                      # Helm charts repository
│   ├── infrastructure/             # PHASE 1: K8s infrastructure components
│   │   ├── networking/
│   │   │   ├── nginx-ingress/
│   │   │   ├── metallb/
│   │   │   ├── calico/
│   │   │   └── istio/
│   │   ├── storage/
│   │   │   ├── local-path-provisioner/
│   │   │   ├── nfs-provisioner/
│   │   │   └── longhorn/
│   │   ├── dns/
│   │   │   ├── coredns/
│   │   │   └── external-dns/
│   │   ├── security/
│   │   │   ├── cert-manager/
│   │   │   ├── sealed-secrets/
│   │   │   └── opa-gatekeeper/
│   │   └── monitoring/
│   │       ├── metrics-server/
│   │       ├── kube-state-metrics/
│   │       └── prometheus-operator/
│   ├── applications/               # PHASE 2: Application templates (future)
│   │   ├── databases/
│   │   │   ├── postgres/
│   │   │   ├── mysql/
│   │   │   └── mongodb/
│   │   ├── web/
│   │   │   ├── nginx/
│   │   │   └── nodejs/
│   │   └── messaging/
│   │       ├── rabbitmq/
│   │       └── kafka/
│   └── base/                       # Base templates
│       └── common/
│           ├── _helpers.tpl
│           └── labels.yaml
├── kustomize/                      # Kustomize overlays
│   ├── base/
│   └── overlays/
│       ├── development/
│       ├── staging/
│       └── production/
└── pkg/
    ├── helm/                       # Helm integration
    │   ├── renderer.go
    │   └── validator.go
    └── kustomize/                  # Kustomize integration
        └── patcher.go
```

## Implementation Flow

### 1. JSON Input from UI (Phase 1 Example - K8s Components)
```json
{
  "workflow": {
    "components": [
      {
        "id": "ingress-controller",
        "templateId": "infrastructure/networking/nginx-ingress",
        "config": {
          "type": "LoadBalancer",
          "replicas": 2
        }
      },
      {
        "id": "cert-manager",
        "templateId": "infrastructure/security/cert-manager",
        "config": {
          "email": "admin@example.com",
          "staging": false
        }
      }
    ],
    "environment": "development"
  }
}
```

### 2. Helm Template Processing
```go
// pkg/helm/renderer.go
package helm

import (
    "helm.sh/helm/v3/pkg/chart"
    "helm.sh/helm/v3/pkg/chart/loader"
    "helm.sh/helm/v3/pkg/engine"
)

type Renderer struct {
    templatesPath string
}

func (r *Renderer) RenderComponent(component Component) (string, error) {
    // Load Helm chart
    chartPath := filepath.Join(r.templatesPath, component.TemplateID)
    chart, err := loader.Load(chartPath)
    
    // Prepare values
    values := map[string]interface{}{
        "component": component.Config,
        "global": map[string]interface{}{
            "namespace": "default",
            "labels": defaultLabels(),
        },
    }
    
    // Render templates
    rendered, err := engine.Render(chart, values)
    return combineManifests(rendered), nil
}
```

### 3. Kustomize Environment Overlay
```go
// pkg/kustomize/patcher.go
package kustomize

import (
    "sigs.k8s.io/kustomize/api/krusty"
    "sigs.k8s.io/kustomize/kyaml/filesys"
)

func ApplyEnvironmentOverlay(manifest string, env string) (string, error) {
    // Write manifest to temp directory
    fs := filesys.MakeFsOnDisk()
    fs.WriteFile("base/manifest.yaml", []byte(manifest))
    
    // Create kustomization.yaml
    kustomization := fmt.Sprintf(`
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - manifest.yaml
patchesStrategicMerge:
  - ../../overlays/%s/patches.yaml
`, env)
    
    fs.WriteFile("base/kustomization.yaml", []byte(kustomization))
    
    // Apply Kustomize
    k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
    resMap, err := k.Run(fs, "base")
    
    // Convert back to YAML
    return resMap.AsYaml()
}
```

## Template Examples

### Phase 1: K8s Component Template (templates/infrastructure/networking/nginx-ingress/templates/deployment.yaml)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-ingress-controller
  namespace: kube-system
  labels:
    app.kubernetes.io/name: nginx-ingress
    app.kubernetes.io/part-of: kubeorchestra
    {{- include "common.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.component.replicas | default 2 }}
  selector:
    matchLabels:
      app.kubernetes.io/name: nginx-ingress
  template:
    metadata:
      labels:
        app.kubernetes.io/name: nginx-ingress
    spec:
      serviceAccountName: nginx-ingress-controller
      containers:
      - name: controller
        image: k8s.gcr.io/ingress-nginx/controller:{{ .Values.component.version | default "v1.8.1" }}
        args:
        - /nginx-ingress-controller
        - --configmap=$(POD_NAMESPACE)/nginx-configuration
        - --tcp-services-configmap=$(POD_NAMESPACE)/tcp-services
        - --udp-services-configmap=$(POD_NAMESPACE)/udp-services
        - --publish-service=$(POD_NAMESPACE)/nginx-ingress-lb
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        ports:
        - name: http
          containerPort: 80
          protocol: TCP
        - name: https
          containerPort: 443
          protocol: TCP
        resources:
          limits:
            cpu: {{ .Values.component.resources.cpu | default "200m" }}
            memory: {{ .Values.component.resources.memory | default "256Mi" }}
```

### Kustomize Overlay (kustomize/overlays/production/patches.yaml)
```yaml
# Increase replicas for production
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-ingress-controller
  namespace: kube-system
spec:
  replicas: 3
---
# Add production-specific resources
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-ingress-controller
  namespace: kube-system
spec:
  template:
    spec:
      containers:
      - name: controller
        resources:
          limits:
            memory: 512Mi
            cpu: 500m
          requests:
            memory: 256Mi
            cpu: 200m
```

## API Endpoints

### 1. List Available Templates
```go
GET /api/v1/templates

Response:
{
  "templates": [
    {
      "id": "infrastructure/networking/nginx-ingress",
      "name": "NGINX Ingress Controller",
      "description": "Production-grade ingress controller for Kubernetes",
      "category": "k8s-component",
      "phase": 1,
      "version": "1.0.0",
      "parameters": [
        {
          "name": "replicas",
          "type": "integer",
          "default": 2,
          "description": "Number of controller replicas"
        },
        {
          "name": "type",
          "type": "string",
          "default": "LoadBalancer",
          "options": ["LoadBalancer", "NodePort", "ClusterIP"]
        }
      ]
    },
    {
      "id": "infrastructure/security/cert-manager",
      "name": "Cert Manager",
      "description": "Automatic TLS certificate management",
      "category": "k8s-component",
      "phase": 1,
      "version": "1.0.0"
    }
  ]
}
```

### 2. Render Workflow
```go
POST /api/v1/render

Request:
{
  "workflow": { ... },
  "environment": "production",
  "dryRun": true
}

Response:
{
  "manifests": "---\napiVersion: apps/v1\n...",
  "validation": {
    "passed": true,
    "warnings": []
  }
}
```

### 3. Deploy Workflow
```go
POST /api/v1/deploy

Request:
{
  "workflow": { ... },
  "environment": "production",
  "cluster": "production-cluster"
}

Response:
{
  "deploymentId": "dep-123",
  "status": "deploying",
  "websocket": "ws://api/v1/deployments/dep-123/stream"
}
```

## Integration with Go Modules

### Required Dependencies
```go
// go.mod
require (
    helm.sh/helm/v3 v3.13.0
    sigs.k8s.io/kustomize/api v0.15.0
    sigs.k8s.io/kustomize/kyaml v0.15.0
    k8s.io/client-go v0.28.0
    k8s.io/apimachinery v0.28.0
)
```

### Template Service Implementation
```go
// services/template_service.go
package services

type TemplateService struct {
    helmRenderer     *helm.Renderer
    kustomizePatcher *kustomize.Patcher
    k8sClient        kubernetes.Interface
}

func (s *TemplateService) ProcessWorkflow(workflow Workflow) (string, error) {
    var manifests []string
    
    // 1. Render each component with Helm
    for _, component := range workflow.Components {
        rendered, err := s.helmRenderer.RenderComponent(component)
        if err != nil {
            return "", fmt.Errorf("helm render failed: %w", err)
        }
        manifests = append(manifests, rendered)
    }
    
    // 2. Combine all manifests
    combined := strings.Join(manifests, "\n---\n")
    
    // 3. Apply Kustomize overlay for environment
    if workflow.Environment != "" {
        patched, err := s.kustomizePatcher.ApplyEnvironmentOverlay(
            combined, 
            workflow.Environment,
        )
        if err != nil {
            return "", fmt.Errorf("kustomize patch failed: %w", err)
        }
        combined = patched
    }
    
    // 4. Validate against cluster
    if err := s.validateManifests(combined); err != nil {
        return "", fmt.Errorf("validation failed: %w", err)
    }
    
    return combined, nil
}
```

## Benefits of This Approach

1. **Best of Both Worlds**: Helm for templating, Kustomize for customization
2. **No Lock-in**: Users can export YAML and use it directly
3. **GitOps Ready**: Generated manifests work with ArgoCD/Flux
4. **Environment Flexibility**: Easy dev/staging/prod variations
5. **Version Control**: Templates are versioned and tracked
6. **Extensible**: Users can add custom templates

## Implementation Phases

### Phase 1: Kubernetes Infrastructure Components (Current Priority)
1. **Networking Components**
   - NGINX Ingress Controller
   - MetalLB Load Balancer
   - Calico CNI
2. **Storage Components**
   - local-path-provisioner
   - NFS provisioner
3. **Security Components**
   - cert-manager
   - sealed-secrets
4. **Monitoring Components**
   - metrics-server
   - kube-state-metrics

### Phase 2: Application Templates (Future)
1. **Databases**: PostgreSQL, MySQL, MongoDB
2. **Web Apps**: NGINX, Node.js applications
3. **Messaging**: RabbitMQ, Kafka
4. **Caching**: Redis, Memcached

## Next Steps

1. **Phase 1 Priority**: Create Helm charts for K8s infrastructure components
2. Build Helm renderer service in Go
3. Implement Kustomize overlay system for environments
4. Add validation against cluster API
5. Test K8s component deployment pipeline
6. **Phase 2**: Add application templates after infrastructure is complete