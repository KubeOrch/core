# Cluster Management API Documentation

## Overview
The Kubernetes cluster management API provides endpoints to manage multiple Kubernetes clusters per user, with support for various authentication methods and cluster sharing.

## Authentication
All endpoints require JWT authentication. Include the token in the Authorization header:
```
Authorization: Bearer <jwt-token>
```

## Endpoints

### Add Cluster
**POST** `/v1/api/clusters`

Add a new Kubernetes cluster configuration.

**Request Body:**
```json
{
  "name": "production",
  "displayName": "Production Cluster",
  "description": "Main production cluster",
  "server": "https://k8s.example.com:6443",
  "authType": "token",
  "credentials": {
    "token": "eyJhbGciOiJSUzI1NiIs...",
    "caData": "LS0tLS1CRUdJTi...",
    "namespace": "default"
  },
  "labels": {
    "environment": "production",
    "region": "us-west-2"
  }
}
```

**Auth Types:**
- `kubeconfig` - Full kubeconfig file
- `token` - Bearer token authentication
- `certificate` - X.509 client certificates
- `serviceaccount` - Service account token
- `oidc` - OpenID Connect

**Response:**
```json
{
  "message": "Cluster added successfully",
  "cluster": {
    "id": "507f1f77bcf86cd799439011",
    "name": "production",
    "status": "connected"
  }
}
```

### List Clusters
**GET** `/v1/api/clusters`

List all clusters accessible by the user.

**Response:**
```json
{
  "clusters": [
    {
      "id": "507f1f77bcf86cd799439011",
      "name": "production",
      "displayName": "Production Cluster",
      "server": "https://k8s.example.com:6443",
      "authType": "token",
      "status": "connected",
      "default": true,
      "metadata": {
        "version": "v1.28.0",
        "nodeCount": 5,
        "platform": "linux",
        "namespaces": ["default", "kube-system"]
      }
    }
  ],
  "default": "507f1f77bcf86cd799439011"
}
```

### Get Cluster
**GET** `/v1/api/clusters/:name`

Get details of a specific cluster.

### Remove Cluster
**DELETE** `/v1/api/clusters/:name`

Remove a cluster configuration.

### Set Default Cluster
**PUT** `/v1/api/clusters/:name/default`

Set a cluster as the default for the user.

### Test Connection
**POST** `/v1/api/clusters/:name/test`

Test connectivity to a cluster.

**Response:**
```json
{
  "message": "Connection test successful",
  "cluster": "production",
  "status": "connected"
}
```

### Refresh Metadata
**POST** `/v1/api/clusters/:name/refresh`

Refresh cluster metadata (version, nodes, namespaces).

### Get Connection Logs
**GET** `/v1/api/clusters/:name/logs?limit=100`

Get audit logs for cluster operations.

### Update Credentials
**PUT** `/v1/api/clusters/:name/credentials`

Update cluster authentication credentials.

**Request Body:**
```json
{
  "credentials": {
    "token": "new-token-here",
    "caData": "new-ca-data"
  }
}
```

### Share Cluster
**POST** `/v1/api/clusters/:name/share`

Share cluster access with another user.

**Request Body:**
```json
{
  "targetUserId": "507f1f77bcf86cd799439012",
  "role": "viewer",
  "namespaces": ["default", "staging"]
}
```

## Authentication Examples

### Token Authentication
```json
{
  "authType": "token",
  "credentials": {
    "token": "eyJhbGciOiJSUzI1NiIs...",
    "caData": "LS0tLS1CRUdJTi..."
  }
}
```

### Certificate Authentication
```json
{
  "authType": "certificate",
  "credentials": {
    "clientCertData": "LS0tLS1CRUdJTi...",
    "clientKeyData": "LS0tLS1CRUdJTi...",
    "caData": "LS0tLS1CRUdJTi..."
  }
}
```

### KubeConfig Authentication
```json
{
  "authType": "kubeconfig",
  "credentials": {
    "kubeconfig": "apiVersion: v1\nkind: Config\n..."
  }
}
```

### OIDC Authentication
```json
{
  "authType": "oidc",
  "credentials": {
    "oidcIssuerUrl": "https://accounts.google.com",
    "oidcClientId": "client-id",
    "oidcClientSecret": "client-secret",
    "oidcRefreshToken": "refresh-token",
    "oidcScopes": ["openid", "email"]
  }
}
```

## Error Responses

### 400 Bad Request
```json
{
  "error": "Invalid request body"
}
```

### 401 Unauthorized
```json
{
  "error": "User not authenticated"
}
```

### 404 Not Found
```json
{
  "error": "Cluster not found"
}
```

### 503 Service Unavailable
```json
{
  "error": "Failed to connect to cluster",
  "cluster": "production",
  "status": "failed"
}
```

## Status Values
- `connected` - Successfully connected
- `disconnected` - Not connected
- `error` - Connection error
- `unknown` - Status not checked