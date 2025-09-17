# Deployment Flow: UI → Kubernetes

## Complete Request Flow for Creating a Deployment

### 1. **UI Creates Workflow** (Initial Setup)
```javascript
POST /v1/api/workflows
{
  "name": "Deploy My App",
  "cluster_id": "prod-cluster-1",
  "nodes": [
    {
      "id": "deploy-node-1",
      "type": "deployment",
      "position": { "x": 100, "y": 200 },
      "data": {
        "deployment": {
          "templateId": "core/deployment",  // Which template to use
          "name": "my-app",
          "image": "nginx:latest",
          "replicas": 3,
          "port": 80,
          "env": {
            "APP_ENV": "production"
          }
        }
      }
    }
  ],
  "edges": []
}
```
**Handler:** `handlers/workflow_handler.go::CreateWorkflowHandler()`
**Service:** `services/workflow_service.go::CreateWorkflow()`
**Storage:** MongoDB `workflows` collection

---

### 2. **UI Publishes Workflow**
```javascript
PUT /v1/api/workflows/{id}/status
{
  "status": "published"
}
```
**Handler:** `handlers/workflow_handler.go::UpdateWorkflowStatusHandler()`
**Purpose:** Workflow must be published before it can run

---

### 3. **UI Triggers Workflow Execution** 🚀
```javascript
POST /v1/api/workflows/{id}/run
```

#### **Detailed Execution Flow:**

```
handlers/workflow_handler.go::RunWorkflowHandler()
    │
    ├─> Creates version snapshot
    │
    └─> services/workflow_executor.go::NewWorkflowExecutor()
        └─> ExecuteWorkflow()
            │
            ├─[1]─> Get Workflow from MongoDB
            │       services/workflow_service.go::GetWorkflowByID()
            │
            ├─[2]─> Create WorkflowRun record
            │       MongoDB: workflow_runs collection
            │
            ├─[3]─> Get Cluster Configuration
            │       getClusterForWorkflow()
            │       └─> repositories/cluster_repository.go::GetAll()
            │           └─> Find cluster by name or default
            │
            ├─[4]─> Build Kubernetes Connection
            │       services/kubernetes_cluster_service.go::clusterToAuthConfig()
            │       └─> pkg/kubernetes/auth.go::BuildRESTConfig()
            │           └─> Creates REST config with Bearer token
            │
            ├─[5]─> Create Manifest Applier
            │       pkg/applier/manifest_applier.go::NewManifestApplier()
            │       └─> Creates dynamic K8s client
            │
            └─[6]─> Execute Each Node (for deployment nodes)
                    executeDeploymentNode()
                    │
                    ├─[6.1]─> Extract deployment data from node
                    │
                    ├─[6.2]─> Prepare template values
                    │         prepareTemplateValues()
                    │         └─> Maps JSON to template variables
                    │
                    ├─[6.3]─> Validate Parameters
                    │         pkg/validator/resource_validator.go::ValidateResourceParams()
                    │         └─> Validates based on resource type
                    │
                    ├─[6.4]─> Render Template
                    │         pkg/template/engine.go::RenderTemplate()
                    │         ├─> Loads: templates/core/deployment/template.yaml
                    │         ├─> Applies Go template with values
                    │         └─> Returns: Raw YAML bytes
                    │
                    ├─[6.5]─> Apply to Kubernetes
                    │         pkg/applier/manifest_applier.go::ApplyYAML()
                    │         ├─> Parses YAML to unstructured objects
                    │         ├─> Uses dynamic client
                    │         ├─> Checks if resource exists
                    │         ├─> Creates or Updates resource
                    │         └─> Returns: ApplyResult with status
                    │
                    └─[6.6]─> Update WorkflowRun
                              ├─> Updates node_states
                              ├─> Adds logs
                              └─> Saves to MongoDB
```

---

## Key Files and Their Roles

### **Entry Points**
- `routes/routes.go` - Defines API endpoints
- `handlers/workflow_handler.go` - HTTP request handlers

### **Business Logic**
- `services/workflow_executor.go` - Main orchestration engine
- `services/workflow_service.go` - Workflow CRUD operations
- `services/kubernetes_cluster_service.go` - Cluster connection management

### **Core Libraries**
- `pkg/template/engine.go` - Renders YAML templates
- `pkg/applier/manifest_applier.go` - Applies YAML to Kubernetes
- `pkg/validator/resource_validator.go` - Validates input parameters

### **Kubernetes Integration**
- `pkg/kubernetes/auth.go` - Authentication (Bearer token, etc.)
- Uses `k8s.io/client-go/dynamic` - Dynamic client for any resource

### **Data Storage**
- `models/workflow.go` - Workflow data structure
- `models/cluster.go` - Cluster configuration
- `database/mongodb.go` - MongoDB connection
- `repositories/cluster_repository.go` - Cluster data access

### **Templates**
- `templates/core/deployment/template.yaml` - Deployment template
- `templates/core/service/template.yaml` - Service template

---

## Example: What Happens with the Data

### Input from UI:
```json
{
  "templateId": "core/deployment",
  "name": "my-app",
  "image": "nginx:latest",
  "replicas": 3
}
```

### After Template Rendering:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
  labels:
    app: my-app
    version: v1
    managed-by: kubeorch
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
        version: v1
    spec:
      containers:
      - name: my-app
        image: nginx:latest
        ports:
        - containerPort: 80
          protocol: TCP
          name: http
```

### Applied to Kubernetes:
```go
// manifest_applier.go does this internally:
dynamicClient.Resource(deployments).
    Namespace("default").
    Create(ctx, unstructuredObj, metav1.CreateOptions{})
```

### Result Stored in MongoDB:
```json
{
  "_id": "workflow_run_id",
  "workflow_id": "workflow_id",
  "status": "completed",
  "node_states": {
    "deploy-node-1": {
      "status": "completed",
      "result": {
        "applied_resources": [{
          "kind": "Deployment",
          "name": "my-app",
          "namespace": "default",
          "operation": "created"
        }]
      }
    }
  }
}
```

---

## Adding New Resource Types

To add a new resource (e.g., Service):

1. **Create Template**: `templates/core/service/template.yaml`
2. **Add Validation** (optional): Update `resource_validator.go`
3. **Use from UI**: `"templateId": "core/service"`

That's it! No other code changes needed.

---

## Security Flow

1. **User Authentication**: JWT token validates user
2. **Workflow Ownership**: User can only run their workflows
3. **Cluster Access**: Bearer token authenticates to K8s cluster
4. **Namespace Isolation**: Resources deployed to specific namespaces

---

## Error Handling

Each step has error handling:
- Template not found → 404 error
- Validation fails → 400 with details
- K8s connection fails → 500 with cluster error
- Apply fails → Logged in workflow_run with error details