package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// RegistryType represents the type of container registry
type RegistryType string

const (
	RegistryTypeDockerHub RegistryType = "dockerhub"
	RegistryTypeGHCR      RegistryType = "ghcr"
	RegistryTypeECR       RegistryType = "ecr"
	RegistryTypeGCR       RegistryType = "gcr"
	RegistryTypeACR       RegistryType = "acr"
	RegistryTypeCustom    RegistryType = "custom"
)

// RegistryStatus represents the connection status of a registry
type RegistryStatus string

const (
	RegistryStatusConnected    RegistryStatus = "connected"
	RegistryStatusDisconnected RegistryStatus = "disconnected"
	RegistryStatusUnknown      RegistryStatus = "unknown"
	RegistryStatusError        RegistryStatus = "error"
)

// Registry represents a container registry credential configuration
type Registry struct {
	ID           primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	Name         string              `bson:"name" json:"name"`
	RegistryType RegistryType        `bson:"registry_type" json:"registryType"`
	RegistryURL  string              `bson:"registry_url" json:"registryUrl"`
	Credentials  RegistryCredentials `bson:"credentials" json:"-"` // Never expose credentials in JSON
	Status       RegistryStatus      `bson:"status" json:"status"`
	LastCheck    time.Time           `bson:"last_check" json:"lastCheck"`
	IsDefault    bool                `bson:"is_default" json:"isDefault"` // Default for this registry type
	CreatedBy    primitive.ObjectID  `bson:"created_by" json:"createdBy"`
	CreatedAt    time.Time           `bson:"created_at" json:"createdAt"`
	UpdatedAt    time.Time           `bson:"updated_at" json:"updatedAt"`
}

// RegistryCredentials holds the authentication credentials for a registry
// All sensitive fields are encrypted at rest
type RegistryCredentials struct {
	// Common fields (for DockerHub, GHCR, Custom)
	Username string `bson:"username,omitempty" json:"username,omitempty"`
	Password string `bson:"password,omitempty" json:"password,omitempty"` // PAT/token

	// AWS ECR specific
	AccessKeyID     string `bson:"access_key_id,omitempty" json:"accessKeyId,omitempty"`
	SecretAccessKey string `bson:"secret_access_key,omitempty" json:"secretAccessKey,omitempty"`
	Region          string `bson:"region,omitempty" json:"region,omitempty"`

	// Google Artifact Registry / GCR specific
	ServiceAccountJSON string `bson:"service_account_json,omitempty" json:"serviceAccountJson,omitempty"`

	// Azure ACR specific
	TenantID     string `bson:"tenant_id,omitempty" json:"tenantId,omitempty"`
	ClientID     string `bson:"client_id,omitempty" json:"clientId,omitempty"`
	ClientSecret string `bson:"client_secret,omitempty" json:"clientSecret,omitempty"`
}

// GetRegistryDomain returns the domain/server URL for the registry
func (r *Registry) GetRegistryDomain() string {
	switch r.RegistryType {
	case RegistryTypeDockerHub:
		return "https://index.docker.io/v1/"
	case RegistryTypeGHCR:
		return "https://ghcr.io"
	case RegistryTypeGCR:
		if r.RegistryURL != "" {
			return r.RegistryURL
		}
		return "https://gcr.io"
	case RegistryTypeECR:
		// Strip any path from ECR URL, keep only domain
		// Input: "254797531501.dkr.ecr.ap-south-1.amazonaws.com/repo/name"
		// Output: "254797531501.dkr.ecr.ap-south-1.amazonaws.com"
		if r.RegistryURL != "" {
			// Remove protocol if present
			url := r.RegistryURL
			url = strings.TrimPrefix(url, "https://")
			url = strings.TrimPrefix(url, "http://")
			// Take only the domain part (before any /)
			if idx := strings.Index(url, "/"); idx > 0 {
				return url[:idx]
			}
			return url
		}
		return r.RegistryURL
	case RegistryTypeACR:
		return r.RegistryURL // ACR URL is registry-specific
	case RegistryTypeCustom:
		return r.RegistryURL
	default:
		return r.RegistryURL
	}
}

// DetectRegistryType detects the registry type from an image reference
func DetectRegistryType(image string) RegistryType {
	// If no domain or no dot in first segment, it's Docker Hub
	if !strings.Contains(image, "/") {
		return RegistryTypeDockerHub
	}

	parts := strings.SplitN(image, "/", 2)
	domain := parts[0]

	// Check if it looks like a domain (contains a dot)
	if !strings.Contains(domain, ".") {
		return RegistryTypeDockerHub
	}

	// Match known registries
	switch {
	case domain == "docker.io" || domain == "index.docker.io":
		return RegistryTypeDockerHub
	case domain == "ghcr.io":
		return RegistryTypeGHCR
	case strings.HasSuffix(domain, ".azurecr.io"):
		return RegistryTypeACR
	case strings.Contains(domain, ".dkr.ecr.") && strings.Contains(domain, ".amazonaws.com"):
		return RegistryTypeECR
	case strings.Contains(domain, "public.ecr.aws"):
		return RegistryTypeECR
	case strings.HasSuffix(domain, ".gcr.io") || strings.HasSuffix(domain, "-docker.pkg.dev"):
		return RegistryTypeGCR
	default:
		return RegistryTypeCustom
	}
}
