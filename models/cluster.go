package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ClusterAuthType string

const (
	ClusterAuthKubeConfig     ClusterAuthType = "kubeconfig"
	ClusterAuthServiceAccount ClusterAuthType = "serviceaccount"
	ClusterAuthToken          ClusterAuthType = "token"
	ClusterAuthCertificate    ClusterAuthType = "certificate"
	ClusterAuthOIDC           ClusterAuthType = "oidc"
)

type ClusterStatus string

const (
	ClusterStatusConnected    ClusterStatus = "connected"
	ClusterStatusDisconnected ClusterStatus = "disconnected"
	ClusterStatusUnknown      ClusterStatus = "unknown"
	ClusterStatusError        ClusterStatus = "error"
)

type Cluster struct {
	ID          primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Name        string               `bson:"name" json:"name"`
	DisplayName string               `bson:"display_name" json:"displayName"`
	Description string               `bson:"description" json:"description"`
	Server      string               `bson:"server" json:"server"`
	AuthType    ClusterAuthType      `bson:"auth_type" json:"authType"`
	Credentials ClusterCredentials   `bson:"credentials" json:"-"`
	Status      ClusterStatus        `bson:"status" json:"status"`
	LastCheck   time.Time            `bson:"last_check" json:"lastCheck"`
	Default     bool                 `bson:"default" json:"default"`
	SingleNode  bool                 `bson:"single_node" json:"singleNode"`
	Labels      map[string]string    `bson:"labels" json:"labels"`
	Annotations map[string]string    `bson:"annotations" json:"annotations"`
	Metadata    ClusterMetadata      `bson:"metadata" json:"metadata"`
	UserID      primitive.ObjectID   `bson:"user_id" json:"userId"`
	OrgID       primitive.ObjectID   `bson:"org_id,omitempty" json:"orgId,omitempty"`
	SharedWith  []primitive.ObjectID `bson:"shared_with" json:"sharedWith"`
	CreatedAt   time.Time            `bson:"created_at" json:"createdAt"`
	UpdatedAt   time.Time            `bson:"updated_at" json:"updatedAt"`
}

type ClusterCredentials struct {
	KubeConfig     string `bson:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`
	Token          string `bson:"token,omitempty" json:"token,omitempty"`
	ClientCertData string `bson:"client_cert_data,omitempty" json:"clientCertData,omitempty"`
	ClientKeyData  string `bson:"client_key_data,omitempty" json:"clientKeyData,omitempty"`
	CAData         string `bson:"ca_data,omitempty" json:"caData,omitempty"`

	OIDCIssuerURL    string   `bson:"oidc_issuer_url,omitempty" json:"oidcIssuerUrl,omitempty"`
	OIDCClientID     string   `bson:"oidc_client_id,omitempty" json:"oidcClientId,omitempty"`
	OIDCClientSecret string   `bson:"oidc_client_secret,omitempty" json:"oidcClientSecret,omitempty"`
	OIDCRefreshToken string   `bson:"oidc_refresh_token,omitempty" json:"oidcRefreshToken,omitempty"`
	OIDCScopes       []string `bson:"oidc_scopes,omitempty" json:"oidcScopes,omitempty"`

	Namespace string `bson:"namespace,omitempty" json:"namespace"`
	Context   string `bson:"context,omitempty" json:"context"`
	Insecure  bool   `bson:"insecure" json:"insecure"`
}

type ClusterMetadata struct {
	Version        string    `bson:"version" json:"version"`
	Platform       string    `bson:"platform" json:"platform"`
	Provider       string    `bson:"provider" json:"provider"`
	Region         string    `bson:"region" json:"region"`
	NodeCount      int       `bson:"node_count" json:"nodeCount"`
	CPUCapacity    string    `bson:"cpu_capacity" json:"cpuCapacity"`
	MemoryCapacity string    `bson:"memory_capacity" json:"memoryCapacity"`
	StorageClasses []string  `bson:"storage_classes" json:"storageClasses"`
	Namespaces     []string  `bson:"namespaces" json:"namespaces"`
	Capabilities   []string  `bson:"capabilities" json:"capabilities"`
	LastUpdated    time.Time `bson:"last_updated" json:"lastUpdated"`
}

type ClusterAccess struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClusterID   primitive.ObjectID `bson:"cluster_id" json:"clusterId"`
	UserID      primitive.ObjectID `bson:"user_id" json:"userId"`
	Role        string             `bson:"role" json:"role"`
	Namespaces  []string           `bson:"namespaces" json:"namespaces"`
	Permissions []string           `bson:"permissions" json:"permissions"`
	GrantedBy   primitive.ObjectID `bson:"granted_by" json:"grantedBy"`
	GrantedAt   time.Time          `bson:"granted_at" json:"grantedAt"`
	ExpiresAt   *time.Time         `bson:"expires_at,omitempty" json:"expiresAt,omitempty"`
}

type ClusterConnectionLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClusterID primitive.ObjectID `bson:"cluster_id" json:"clusterId"`
	UserID    primitive.ObjectID `bson:"user_id" json:"userId"`
	Action    string             `bson:"action" json:"action"`
	Success   bool               `bson:"success" json:"success"`
	Error     string             `bson:"error,omitempty" json:"error,omitempty"`
	Details   map[string]any     `bson:"details,omitempty" json:"details,omitempty"`
	Timestamp time.Time          `bson:"timestamp" json:"timestamp"`
}
