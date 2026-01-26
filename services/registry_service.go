package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RegistryService struct {
	repo   *repositories.RegistryRepository
	logger *logrus.Logger
}

func NewRegistryService() *RegistryService {
	return &RegistryService{
		repo:   repositories.NewRegistryRepository(),
		logger: logrus.New(),
	}
}

// CreateRegistry creates a new registry credential
func (s *RegistryService) CreateRegistry(ctx context.Context, registry *models.Registry) error {
	// Validate registry type
	if err := s.validateRegistry(registry); err != nil {
		return err
	}

	// Save to database
	if err := s.repo.Create(ctx, registry); err != nil {
		return err
	}

	// Test connection asynchronously
	go func() {
		testCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.testConnection(testCtx, registry); err != nil {
			s.logger.WithFields(logrus.Fields{
				"registry": registry.Name,
				"type":     registry.RegistryType,
				"error":    err.Error(),
			}).Warn("Registry connection test failed")
			_ = s.repo.UpdateStatus(testCtx, registry.ID, models.RegistryStatusError)
		} else {
			s.logger.WithFields(logrus.Fields{
				"registry": registry.Name,
				"type":     registry.RegistryType,
			}).Info("Registry connection test successful")
			_ = s.repo.UpdateStatus(testCtx, registry.ID, models.RegistryStatusConnected)
		}
	}()

	return nil
}

// GetRegistry retrieves a registry by ID
func (s *RegistryService) GetRegistry(ctx context.Context, id primitive.ObjectID) (*models.Registry, error) {
	return s.repo.GetByID(ctx, id)
}

// GetRegistryByName retrieves a registry by name
func (s *RegistryService) GetRegistryByName(ctx context.Context, name string) (*models.Registry, error) {
	return s.repo.GetByName(ctx, name)
}

// ListRegistries returns all registries
func (s *RegistryService) ListRegistries(ctx context.Context) ([]*models.Registry, error) {
	return s.repo.List(ctx)
}

// GetRegistriesByType returns registries of a specific type
func (s *RegistryService) GetRegistriesByType(ctx context.Context, registryType models.RegistryType) ([]*models.Registry, error) {
	return s.repo.GetByType(ctx, registryType)
}

// GetDefaultRegistry returns the default registry for a specific type
func (s *RegistryService) GetDefaultRegistry(ctx context.Context, registryType models.RegistryType) (*models.Registry, error) {
	return s.repo.GetDefaultByType(ctx, registryType)
}

// GetRegistryForImage finds the appropriate registry for a given image
func (s *RegistryService) GetRegistryForImage(ctx context.Context, image string) (*models.Registry, error) {
	registryType := models.DetectRegistryType(image)

	// Try to get default registry for this type
	registry, err := s.repo.GetDefaultByType(ctx, registryType)
	if err != nil {
		return nil, err
	}

	if registry != nil {
		return registry, nil
	}

	// If no default, get any registry of this type
	registries, err := s.repo.GetByType(ctx, registryType)
	if err != nil {
		return nil, err
	}

	if len(registries) == 0 {
		return nil, nil // No registry configured for this type
	}

	return registries[0], nil
}

// UpdateRegistry updates a registry
func (s *RegistryService) UpdateRegistry(ctx context.Context, id primitive.ObjectID, updates map[string]interface{}) error {
	// Convert to bson.M
	updateBson := bson.M{}
	for k, v := range updates {
		updateBson[k] = v
	}

	return s.repo.Update(ctx, id, updateBson)
}

// UpdateRegistryCredentials updates only the credentials of a registry
func (s *RegistryService) UpdateRegistryCredentials(ctx context.Context, id primitive.ObjectID, credentials models.RegistryCredentials) error {
	return s.repo.Update(ctx, id, bson.M{"credentials": credentials})
}

// SetDefaultRegistry sets a registry as the default for its type
func (s *RegistryService) SetDefaultRegistry(ctx context.Context, id primitive.ObjectID) error {
	registry, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	return s.repo.SetDefault(ctx, id, registry.RegistryType)
}

// DeleteRegistry deletes a registry
func (s *RegistryService) DeleteRegistry(ctx context.Context, id primitive.ObjectID) error {
	return s.repo.Delete(ctx, id)
}

// TestRegistryConnection tests the connection to a registry
func (s *RegistryService) TestRegistryConnection(ctx context.Context, id primitive.ObjectID) error {
	registry, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	err = s.testConnection(ctx, registry)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, id, models.RegistryStatusError)
		return err
	}

	_ = s.repo.UpdateStatus(ctx, id, models.RegistryStatusConnected)
	return nil
}

// GenerateDockerConfigJSON generates the Docker config JSON for a registry
// This is used to create Kubernetes image pull secrets
func (s *RegistryService) GenerateDockerConfigJSON(ctx context.Context, registry *models.Registry) ([]byte, error) {
	server := registry.GetRegistryDomain()
	username, password, err := s.getAuthCredentials(ctx, registry)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth credentials: %w", err)
	}

	if username == "" || password == "" {
		return nil, fmt.Errorf("registry credentials are incomplete")
	}

	// Create the auth token (base64 encoded username:password)
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	// Create the Docker config structure
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			server: map[string]interface{}{
				"username": username,
				"password": password,
				"auth":     auth,
			},
		},
	}

	return json.Marshal(dockerConfig)
}

// Helper functions

func (s *RegistryService) validateRegistry(registry *models.Registry) error {
	if registry.Name == "" {
		return fmt.Errorf("registry name is required")
	}

	switch registry.RegistryType {
	case models.RegistryTypeDockerHub, models.RegistryTypeGHCR:
		if registry.Credentials.Username == "" || registry.Credentials.Password == "" {
			return fmt.Errorf("username and password/token are required")
		}
	case models.RegistryTypeECR:
		if registry.Credentials.AccessKeyID == "" || registry.Credentials.SecretAccessKey == "" {
			return fmt.Errorf("AWS access key ID and secret access key are required")
		}
		if registry.Credentials.Region == "" {
			return fmt.Errorf("AWS region is required")
		}
		if registry.RegistryURL == "" {
			return fmt.Errorf("ECR registry URL is required")
		}
	case models.RegistryTypeGCR:
		if registry.Credentials.ServiceAccountJSON == "" {
			return fmt.Errorf("service account JSON is required")
		}
	case models.RegistryTypeACR:
		if registry.RegistryURL == "" {
			return fmt.Errorf("ACR registry URL is required")
		}
		if registry.Credentials.ClientID == "" || registry.Credentials.ClientSecret == "" {
			return fmt.Errorf("client ID and client secret are required")
		}
	case models.RegistryTypeCustom:
		if registry.RegistryURL == "" {
			return fmt.Errorf("registry URL is required")
		}
		if registry.Credentials.Username == "" || registry.Credentials.Password == "" {
			return fmt.Errorf("username and password are required")
		}
	default:
		return fmt.Errorf("unsupported registry type: %s", registry.RegistryType)
	}

	return nil
}

func (s *RegistryService) testConnection(ctx context.Context, registry *models.Registry) error {
	switch registry.RegistryType {
	case models.RegistryTypeDockerHub:
		return s.testDockerHubConnection(ctx, registry)
	case models.RegistryTypeGHCR:
		return s.testGHCRConnection(ctx, registry)
	case models.RegistryTypeECR:
		return s.testECRConnection(ctx, registry)
	case models.RegistryTypeGCR:
		return s.testGCRConnection(ctx, registry)
	case models.RegistryTypeACR:
		return s.testACRConnection(ctx, registry)
	case models.RegistryTypeCustom:
		return s.testCustomRegistryConnection(ctx, registry)
	default:
		return fmt.Errorf("unsupported registry type for connection test: %s", registry.RegistryType)
	}
}

func (s *RegistryService) testDockerHubConnection(ctx context.Context, registry *models.Registry) error {
	// Docker Hub uses a token-based auth flow
	// First, get a token using username/password
	req, err := http.NewRequestWithContext(ctx, "GET", "https://hub.docker.com/v2/users/login", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Try the v2 API with basic auth
	req, err = http.NewRequestWithContext(ctx, "GET", "https://registry-1.docker.io/v2/", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(registry.Credentials.Username + ":" + registry.Credentials.Password))
	req.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	// 200 or 401 with Www-Authenticate header is expected
	// Docker Hub returns 401 initially with a Www-Authenticate header to get a token
	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		// For a proper test, we'd need to follow the OAuth flow
		// For now, if we can reach the endpoint, consider it a success
		return nil
	}

	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (s *RegistryService) testGHCRConnection(ctx context.Context, registry *models.Registry) error {
	// GHCR uses basic auth with username and PAT
	req, err := http.NewRequestWithContext(ctx, "GET", "https://ghcr.io/v2/", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(registry.Credentials.Username + ":" + registry.Credentials.Password))
	req.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		return nil
	}

	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (s *RegistryService) testECRConnection(ctx context.Context, registry *models.Registry) error {
	if registry.RegistryURL == "" {
		return fmt.Errorf("ECR registry URL is required")
	}

	// Actually test by getting auth token from AWS ECR API
	_, _, err := s.getECRAuthToken(ctx, registry)
	if err != nil {
		return fmt.Errorf("ECR authentication failed: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"registry": registry.Name,
		"url":      registry.RegistryURL,
	}).Debug("ECR connection test successful")

	return nil
}

func (s *RegistryService) testGCRConnection(ctx context.Context, registry *models.Registry) error {
	// GCR uses service account JSON
	// For a proper test, we'd need to parse the JSON and use it for OAuth
	// For now, just validate the JSON is valid
	var sa map[string]interface{}
	if err := json.Unmarshal([]byte(registry.Credentials.ServiceAccountJSON), &sa); err != nil {
		return fmt.Errorf("invalid service account JSON: %w", err)
	}

	// Check for required fields
	requiredFields := []string{"client_email", "private_key", "project_id"}
	for _, field := range requiredFields {
		if _, ok := sa[field]; !ok {
			return fmt.Errorf("service account JSON missing required field: %s", field)
		}
	}

	return nil
}

func (s *RegistryService) testACRConnection(ctx context.Context, registry *models.Registry) error {
	// ACR uses Azure AD authentication
	// For a proper test, we'd need Azure SDK
	// For now, just validate the URL is reachable
	if registry.RegistryURL == "" {
		return fmt.Errorf("ACR registry URL is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", registry.RegistryURL+"/v2/", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	// 401 is expected without proper auth
	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		return nil
	}

	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (s *RegistryService) testCustomRegistryConnection(ctx context.Context, registry *models.Registry) error {
	if registry.RegistryURL == "" {
		return fmt.Errorf("registry URL is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", registry.RegistryURL+"/v2/", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if registry.Credentials.Username != "" && registry.Credentials.Password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(registry.Credentials.Username + ":" + registry.Credentials.Password))
		req.Header.Set("Authorization", "Basic "+auth)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		return nil
	}

	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

// getECRAuthToken retrieves a temporary auth token from AWS ECR
func (s *RegistryService) getECRAuthToken(ctx context.Context, registry *models.Registry) (username, password string, err error) {
	// Create AWS config with static credentials
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(registry.Credentials.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			registry.Credentials.AccessKeyID,
			registry.Credentials.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create ECR client and get authorization token
	client := ecr.NewFromConfig(cfg)
	output, err := client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get ECR auth token: %w", err)
	}

	if len(output.AuthorizationData) == 0 {
		return "", "", fmt.Errorf("no authorization data returned from ECR")
	}

	// Token is base64 encoded "AWS:password"
	authToken := *output.AuthorizationData[0].AuthorizationToken
	decoded, err := base64.StdEncoding.DecodeString(authToken)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode auth token: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid auth token format")
	}

	s.logger.WithFields(logrus.Fields{
		"registry": registry.Name,
		"region":   registry.Credentials.Region,
	}).Debug("Successfully obtained ECR auth token")

	return parts[0], parts[1], nil // "AWS", <token>
}

func (s *RegistryService) getAuthCredentials(ctx context.Context, registry *models.Registry) (username, password string, err error) {
	switch registry.RegistryType {
	case models.RegistryTypeDockerHub, models.RegistryTypeGHCR, models.RegistryTypeCustom:
		return registry.Credentials.Username, registry.Credentials.Password, nil
	case models.RegistryTypeGCR:
		// GCR uses _json_key as username and the service account JSON as password
		return "_json_key", registry.Credentials.ServiceAccountJSON, nil
	case models.RegistryTypeACR:
		// ACR uses client ID and client secret
		return registry.Credentials.ClientID, registry.Credentials.ClientSecret, nil
	case models.RegistryTypeECR:
		// ECR requires temporary token from AWS API
		return s.getECRAuthToken(ctx, registry)
	default:
		return "", "", fmt.Errorf("unsupported registry type: %s", registry.RegistryType)
	}
}

// Singleton instance
var (
	registryServiceInstance *RegistryService
	registryServiceOnce     sync.Once
)

func GetRegistryService() *RegistryService {
	registryServiceOnce.Do(func() {
		registryServiceInstance = NewRegistryService()
	})
	return registryServiceInstance
}
