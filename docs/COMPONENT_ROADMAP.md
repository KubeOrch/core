# Kubernetes Component Roadmap

This document outlines the plan for implementing additional Kubernetes components in the KubeOrch workflow system.

## Overview

KubeOrch currently supports **Deployment** and **Service** components. This roadmap prioritizes future components based on:
1. **Usage Frequency** - Most commonly used K8s resources
2. **Implementation Complexity** - Ease of implementation
3. **Dependencies** - Independence from other components

## Implementation Status

### ✅ Completed Components

#### 1. Deployment (core/deployment)
- **Status**: ✅ Implemented
- **Category**: Core Workload
- **Usage**: Very High
- **Complexity**: Medium
- **Description**: Deploy and manage containerized applications with automatic rollouts and rollbacks
- **Template Location**: `/templates/core/deployment/`

#### 2. Service (core/service)
- **Status**: ✅ Implemented
- **Category**: Networking
- **Usage**: High
- **Complexity**: Easy
- **Description**: Expose applications on a network with load balancing and service discovery
- **Template Location**: `/templates/core/service/`

---

## Priority Matrix

Components are prioritized in 4 tiers based on usage frequency, complexity, and dependencies:

### **Phase 1: Essential Components** (High Usage + Low Complexity)
These are fundamental K8s resources that should be implemented next.

### **Phase 2: Common Workloads** (High Usage + Medium Complexity)
Frequently used workload types with moderate implementation effort.

### **Phase 3: Advanced Networking & Storage** (Medium Usage + Medium Complexity)
More specialized resources for networking and persistent storage.

### **Phase 4: Specialized Components** (Variable Usage + High Complexity)
Advanced features and specialized use cases.

---

## Phase 1: Essential Components

### 3. ConfigMap (core/configmap)
- **Priority**: 🔴 High
- **Category**: Configuration
- **Usage**: Very High
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Store non-confidential configuration data in key-value pairs
- **Common Use Cases**:
  - Application configuration files
  - Environment-specific settings
  - Command-line arguments

**Implementation Guide**:
```yaml
# Template parameters:
- name: ConfigMap name
- namespace: Target namespace
- data: Key-value pairs (map[string]string)
- binaryData: Binary data (optional)
- labels: Resource labels
```

### 4. Secret (core/secret)
- **Priority**: 🔴 High
- **Category**: Security
- **Usage**: Very High
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Store and manage sensitive information like passwords, tokens, and keys
- **Common Use Cases**:
  - Database credentials
  - API keys and tokens
  - TLS certificates

**Implementation Guide**:
```yaml
# Template parameters:
- name: Secret name
- namespace: Target namespace
- type: Secret type (Opaque, kubernetes.io/tls, etc.)
- data: Base64-encoded key-value pairs
- stringData: Plain-text key-value pairs (auto-encoded)
- labels: Resource labels
```

**Security Note**: Ensure secrets are never logged or exposed in workflow run outputs.

### 5. Namespace (core/namespace)
- **Priority**: 🔴 High
- **Category**: Organization
- **Usage**: High
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Virtual clusters for resource isolation and organization
- **Common Use Cases**:
  - Environment separation (dev/staging/prod)
  - Team workspaces
  - Multi-tenancy

**Implementation Guide**:
```yaml
# Template parameters:
- name: Namespace name
- labels: Resource labels
- annotations: Resource annotations
```

---

## Phase 2: Common Workloads

### 6. StatefulSet (core/statefulset)
- **Priority**: 🟡 Medium-High
- **Category**: Workload
- **Usage**: Medium-High
- **Complexity**: Medium
- **Dependencies**: May require PersistentVolumeClaim
- **Description**: Manage stateful applications with stable network identities and persistent storage
- **Common Use Cases**:
  - Databases (MySQL, PostgreSQL, MongoDB)
  - Distributed systems (Kafka, Elasticsearch)
  - Stateful applications requiring stable hostnames

**Implementation Guide**:
```yaml
# Template parameters:
- name: StatefulSet name
- namespace: Target namespace
- serviceName: Governing service name
- replicas: Number of replicas
- image: Container image
- volumeClaimTemplates: PVC templates for persistent storage
- env: Environment variables
- resources: CPU/memory limits
```

### 7. DaemonSet (core/daemonset)
- **Priority**: 🟡 Medium
- **Category**: Workload
- **Usage**: Medium
- **Complexity**: Medium
- **Dependencies**: None
- **Description**: Run a copy of a pod on all (or selected) nodes
- **Common Use Cases**:
  - Log collectors (Fluentd, Logstash)
  - Monitoring agents (Node Exporter, Datadog)
  - Network plugins (Calico, Weave)

**Implementation Guide**:
```yaml
# Template parameters:
- name: DaemonSet name
- namespace: Target namespace
- image: Container image
- nodeSelector: Node selection criteria
- tolerations: Node taint tolerations
- resources: CPU/memory limits
- env: Environment variables
```

### 8. Job (core/job)
- **Priority**: 🟡 Medium-High
- **Category**: Workload
- **Usage**: Medium-High
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Run pods to completion for batch processing or one-time tasks
- **Common Use Cases**:
  - Database migrations
  - Batch data processing
  - Backup operations

**Implementation Guide**:
```yaml
# Template parameters:
- name: Job name
- namespace: Target namespace
- image: Container image
- command: Command to execute
- completions: Number of successful completions required
- parallelism: Number of pods to run in parallel
- backoffLimit: Number of retries before marking as failed
- ttlSecondsAfterFinished: Cleanup delay after completion
```

### 9. CronJob (core/cronjob)
- **Priority**: 🟡 Medium
- **Category**: Workload
- **Usage**: Medium
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Schedule jobs to run at specific times or intervals
- **Common Use Cases**:
  - Scheduled backups
  - Report generation
  - Periodic cleanup tasks

**Implementation Guide**:
```yaml
# Template parameters:
- name: CronJob name
- namespace: Target namespace
- schedule: Cron schedule expression
- image: Container image
- command: Command to execute
- concurrencyPolicy: Allow/Forbid/Replace concurrent runs
- successfulJobsHistoryLimit: Number of successful jobs to retain
- failedJobsHistoryLimit: Number of failed jobs to retain
```

---

## Phase 3: Advanced Networking & Storage

### 10. Ingress (networking/ingress)
- **Priority**: 🟡 Medium-High
- **Category**: Networking
- **Usage**: High
- **Complexity**: Medium
- **Dependencies**: Requires Ingress Controller
- **Description**: Manage external HTTP/HTTPS access to services
- **Common Use Cases**:
  - Domain-based routing
  - SSL/TLS termination
  - Path-based routing

**Implementation Guide**:
```yaml
# Template parameters:
- name: Ingress name
- namespace: Target namespace
- ingressClassName: Ingress controller class
- rules: Routing rules (host, path, service)
- tls: TLS configuration (optional)
- annotations: Ingress-specific annotations
```

### 11. NetworkPolicy (networking/networkpolicy)
- **Priority**: 🟢 Medium
- **Category**: Security/Networking
- **Usage**: Medium
- **Complexity**: Medium
- **Dependencies**: Requires CNI with NetworkPolicy support
- **Description**: Control network traffic between pods
- **Common Use Cases**:
  - Microservice isolation
  - Security zones
  - Compliance requirements

**Implementation Guide**:
```yaml
# Template parameters:
- name: NetworkPolicy name
- namespace: Target namespace
- podSelector: Pods to which policy applies
- policyTypes: Ingress/Egress
- ingress: Ingress rules
- egress: Egress rules
```

### 12. PersistentVolumeClaim (storage/pvc)
- **Priority**: 🟡 Medium-High
- **Category**: Storage
- **Usage**: Medium-High
- **Complexity**: Easy
- **Dependencies**: Requires PersistentVolume or StorageClass
- **Description**: Request persistent storage for pods
- **Common Use Cases**:
  - Database storage
  - Shared file systems
  - Application data persistence

**Implementation Guide**:
```yaml
# Template parameters:
- name: PVC name
- namespace: Target namespace
- storageClassName: Storage class (optional)
- accessModes: ReadWriteOnce/ReadOnlyMany/ReadWriteMany
- storage: Storage size (e.g., "10Gi")
- volumeMode: Filesystem/Block
- selector: Label selector for PV binding (optional)
```

### 13. StorageClass (storage/storageclass)
- **Priority**: 🟢 Medium
- **Category**: Storage
- **Usage**: Medium
- **Complexity**: Medium
- **Dependencies**: Requires CSI driver or cloud provider
- **Description**: Define classes of storage with different characteristics
- **Common Use Cases**:
  - SSD vs HDD storage tiers
  - Regional storage
  - Backup policies

**Implementation Guide**:
```yaml
# Template parameters:
- name: StorageClass name
- provisioner: Storage provisioner (e.g., kubernetes.io/aws-ebs)
- parameters: Provisioner-specific parameters
- reclaimPolicy: Delete/Retain
- volumeBindingMode: Immediate/WaitForFirstConsumer
- allowVolumeExpansion: Enable/disable volume expansion
```

---

## Phase 4: Specialized Components

### 14. HorizontalPodAutoscaler (autoscaling/hpa)
- **Priority**: 🟢 Medium
- **Category**: Autoscaling
- **Usage**: Medium
- **Complexity**: Medium
- **Dependencies**: Requires Metrics Server
- **Description**: Automatically scale pod replicas based on metrics
- **Common Use Cases**:
  - CPU-based scaling
  - Memory-based scaling
  - Custom metrics scaling

**Implementation Guide**:
```yaml
# Template parameters:
- name: HPA name
- namespace: Target namespace
- scaleTargetRef: Target deployment/statefulset
- minReplicas: Minimum replicas
- maxReplicas: Maximum replicas
- metrics: Scaling metrics (CPU, memory, custom)
- behavior: Scaling behavior policies (optional)
```

### 15. ServiceAccount (core/serviceaccount)
- **Priority**: 🟢 Medium
- **Category**: Security
- **Usage**: Medium
- **Complexity**: Easy
- **Dependencies**: Often used with RBAC
- **Description**: Provide identity for pods to interact with the API server
- **Common Use Cases**:
  - CI/CD pipelines
  - Operators and controllers
  - Service-to-service authentication

**Implementation Guide**:
```yaml
# Template parameters:
- name: ServiceAccount name
- namespace: Target namespace
- automountServiceAccountToken: Auto-mount token
- imagePullSecrets: Image pull secret references
- secrets: Additional secret references
```

### 16. Role / ClusterRole (rbac/role)
- **Priority**: 🟢 Medium
- **Category**: Security/RBAC
- **Usage**: Medium
- **Complexity**: Medium
- **Dependencies**: None (but used with RoleBinding)
- **Description**: Define permissions for resource access
- **Common Use Cases**:
  - Application permissions
  - CI/CD access control
  - Multi-tenancy

**Implementation Guide**:
```yaml
# Template parameters:
- name: Role name
- namespace: Target namespace (omit for ClusterRole)
- rules: Permission rules (apiGroups, resources, verbs)
- aggregationRule: Label selector for aggregated roles (ClusterRole only)
```

### 17. RoleBinding / ClusterRoleBinding (rbac/rolebinding)
- **Priority**: 🟢 Medium
- **Category**: Security/RBAC
- **Usage**: Medium
- **Complexity**: Easy
- **Dependencies**: Requires Role/ClusterRole and ServiceAccount
- **Description**: Bind roles to users, groups, or service accounts
- **Common Use Cases**:
  - Grant permissions to service accounts
  - User access control
  - Team-based access

**Implementation Guide**:
```yaml
# Template parameters:
- name: RoleBinding name
- namespace: Target namespace (omit for ClusterRoleBinding)
- roleRef: Reference to Role/ClusterRole
- subjects: Users, groups, or service accounts to bind
```

### 18. ResourceQuota (core/resourcequota)
- **Priority**: 🔵 Low
- **Category**: Resource Management
- **Usage**: Low-Medium
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Limit resource consumption per namespace
- **Common Use Cases**:
  - Multi-tenancy resource limits
  - Cost control
  - Prevent resource exhaustion

**Implementation Guide**:
```yaml
# Template parameters:
- name: ResourceQuota name
- namespace: Target namespace
- hard: Hard limits (CPU, memory, pods, etc.)
- scopes: Quota scopes (optional)
- scopeSelector: Scope selector (optional)
```

### 19. LimitRange (core/limitrange)
- **Priority**: 🔵 Low
- **Category**: Resource Management
- **Usage**: Low-Medium
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Set default and maximum resource limits for pods/containers
- **Common Use Cases**:
  - Enforce minimum/maximum resources
  - Set defaults for containers
  - Prevent resource hogging

**Implementation Guide**:
```yaml
# Template parameters:
- name: LimitRange name
- namespace: Target namespace
- limits: Limit specifications (type, min, max, default, defaultRequest)
```

### 20. PodDisruptionBudget (policy/pdb)
- **Priority**: 🔵 Low-Medium
- **Category**: Availability
- **Usage**: Low-Medium
- **Complexity**: Easy
- **Dependencies**: None
- **Description**: Ensure minimum number of pods remain available during disruptions
- **Common Use Cases**:
  - Node draining
  - Cluster upgrades
  - High availability requirements

**Implementation Guide**:
```yaml
# Template parameters:
- name: PDB name
- namespace: Target namespace
- selector: Pod selector labels
- minAvailable: Minimum available pods (number or percentage)
- maxUnavailable: Maximum unavailable pods (number or percentage)
```

---

## Implementation Workflow

For each component, follow this standardized workflow:

### 1. Backend Implementation
1. **Create Template** (`/templates/{category}/{component}/template.yaml`)
   - Write Go template with K8s manifest structure
   - Use template variables for user-configurable parameters
   - Follow existing template patterns

2. **Create Metadata** (`/templates/{category}/{component}/metadata.yaml`)
   - Define template ID, name, description
   - Specify category and tags
   - List all parameters with types and validation rules
   - Provide usage examples

3. **Update Executor** (`/services/workflow_executor.go`)
   - Add case to switch statement for new node type
   - Implement `execute{Component}Node()` method
   - Implement `prepare{Component}TemplateValues()` method
   - Follow existing patterns from Deployment/Service

4. **Test Backend**
   - Verify template renders correctly
   - Test validation rules
   - Test workflow execution

### 2. Frontend Implementation
1. **Create Node Component** (`/ui/components/workflow/{Component}Node.tsx`)
   - Follow ReactFlow custom node pattern
   - Display component-specific information
   - Add icons and styling

2. **Create Settings Panel** (`/ui/components/workflow/{Component}SettingsPanel.tsx`)
   - Build dynamic form based on metadata parameters
   - Implement validation
   - Handle parameter types (string, number, select, object, array)

3. **Update TypeScript Types** (`/ui/lib/types/workflow.ts`)
   - Add component type to NodeType union
   - Define component-specific data interfaces

4. **Register in Palette**
   - Add to CommandPalette component template list
   - Include in search/filter functionality

### 3. Testing
1. Create test workflow with component
2. Verify form validation
3. Test template rendering
4. Execute workflow on test cluster
5. Verify resource creation in Kubernetes

---

## Notes

- **Template Registry**: All templates are automatically discovered by the registry system
- **Validation**: Use the validator package to enforce parameter requirements
- **Metadata-Driven UI**: Frontend forms are generated from metadata.yaml
- **Extensibility**: Follow the same pattern for custom/third-party components

---

## Getting Started

To implement a new component:

1. Choose a component from the roadmap
2. Create template and metadata files in `/templates/{category}/{component}/`
3. Update the workflow executor to handle the new component type
4. Create frontend components for the workflow canvas
5. Test end-to-end functionality
6. Update this roadmap document

For questions or guidance, refer to existing Deployment/Service implementations as reference patterns.
