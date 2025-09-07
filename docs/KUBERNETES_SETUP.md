# Kubernetes Client Connection Guide

This guide explains how to connect to a Kubernetes cluster using various authentication methods.

## Quick Start - Service Account Token

### Generate Token (Run on Kubernetes Server)

```bash
# Create service account
kubectl create serviceaccount kubeorch-admin -n kube-system

# Grant cluster-admin permissions
kubectl create clusterrolebinding kubeorch-admin-binding \
  --clusterrole=cluster-admin \
  --serviceaccount=kube-system:kubeorch-admin

# Create long-lived token (10 years)
kubectl create token kubeorch-admin -n kube-system --duration=87600h
```


### Method 2: Service Account with Limited Permissions

```bash
# Create namespace and service account
kubectl create namespace kubeorch-system
kubectl create serviceaccount kubeorch-operator -n kubeorch-system

# Create custom role with specific permissions
cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubeorch-operator
rules:
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets", "statefulsets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["batch"]
  resources: ["jobs", "cronjobs"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubeorch-operator-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubeorch-operator
subjects:
- kind: ServiceAccount
  name: kubeorch-operator
  namespace: kubeorch-system
EOF

# Generate token
kubectl create token kubeorch-operator -n kubeorch-system --duration=8760h
```

### Method 3: Creating Kubeconfig with Token

```bash
# Get cluster details
CLUSTER_ENDPOINT=$(kubectl config view -o jsonpath='{.clusters[0].cluster.server}')
CA_CERT=$(kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
TOKEN=$(kubectl create token kubeorch-admin -n kube-system --duration=87600h)

# Create kubeconfig file
cat <<EOF > kubeorch-config.yaml
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA_CERT}
    server: ${CLUSTER_ENDPOINT}
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubeorch-admin
  name: kubeorch
current-context: kubeorch
users:
- name: kubeorch-admin
  user:
    token: ${TOKEN}
EOF

# Test the kubeconfig
kubectl --kubeconfig=kubeorch-config.yaml get nodes
```

## Get CA Certificate for Production

```bash
# Extract CA certificate from cluster
kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d > ca.crt

# Or get it as base64 string for direct use
kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}'
```
