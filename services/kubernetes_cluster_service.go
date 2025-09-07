package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/KubeOrch/core/models"
	k8sauth "github.com/KubeOrch/core/pkg/kubernetes"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type KubernetesClusterService struct {
	clusterRepo *repositories.ClusterRepository
	logger      *logrus.Logger
}

func NewKubernetesClusterService() *KubernetesClusterService {
	return &KubernetesClusterService{
		clusterRepo: repositories.NewClusterRepository(),
		logger:      logrus.New(),
	}
}

// CreateClusterConnection creates a Kubernetes client from stored cluster credentials
func (s *KubernetesClusterService) CreateClusterConnection(cluster *models.Cluster) (*kubernetes.Clientset, error) {
	auth := s.clusterToAuthConfig(cluster)

	config, err := auth.BuildRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// AddCluster adds a new cluster to the database
func (s *KubernetesClusterService) AddCluster(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster) error {
	cluster.UserID = userID
	cluster.Status = models.ClusterStatusUnknown

	// Test connection before saving
	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		cluster.Status = models.ClusterStatusError
		s.logger.WithError(err).Warn("Failed to create cluster connection")
	} else {
		// Validate connection
		_, err = clientset.Discovery().ServerVersion()
		if err != nil {
			cluster.Status = models.ClusterStatusError
		} else {
			cluster.Status = models.ClusterStatusConnected
			// Update metadata
			if err := s.updateClusterMetadata(ctx, cluster, clientset); err != nil {
				s.logger.WithError(err).Warn("Failed to update cluster metadata on add")
			}
		}
	}

	if err := s.clusterRepo.Create(ctx, cluster); err != nil {
		return fmt.Errorf("failed to save cluster: %w", err)
	}

	s.logConnection(ctx, cluster.ID, userID, "cluster_added", cluster.Status == models.ClusterStatusConnected, nil)

	return nil
}

// GetCluster retrieves a cluster and creates a connection
func (s *KubernetesClusterService) GetClusterClient(ctx context.Context, userID primitive.ObjectID, clusterName string) (*kubernetes.Clientset, error) {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return nil, fmt.Errorf("cluster not found: %w", err)
	}

	return s.CreateClusterConnection(cluster)
}

// GetClusterByID retrieves a cluster by ID
func (s *KubernetesClusterService) GetClusterByID(ctx context.Context, clusterID primitive.ObjectID) (*models.Cluster, error) {
	return s.clusterRepo.GetByID(ctx, clusterID)
}

// GetClusterByName retrieves a cluster by name for a user
func (s *KubernetesClusterService) GetClusterByName(ctx context.Context, userID primitive.ObjectID, name string) (*models.Cluster, error) {
	return s.clusterRepo.GetByName(ctx, name, userID)
}

// ListUserClusters lists all clusters accessible by a user
func (s *KubernetesClusterService) ListUserClusters(ctx context.Context, userID primitive.ObjectID) ([]*models.Cluster, error) {
	return s.clusterRepo.ListByUser(ctx, userID)
}

// RemoveCluster removes a cluster from the database
func (s *KubernetesClusterService) RemoveCluster(ctx context.Context, userID primitive.ObjectID, clusterName string) error {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	// Check permissions
	if cluster.UserID != userID {
		access, err := s.clusterRepo.GetUserAccess(ctx, cluster.ID, userID)
		if err != nil {
			return fmt.Errorf("failed to check user access permissions: %w", err)
		}
		if access == nil || access.Role != "admin" {
			return fmt.Errorf("insufficient permissions to remove cluster")
		}
	}

	if err := s.clusterRepo.Delete(ctx, cluster.ID); err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}

	s.logConnection(ctx, cluster.ID, userID, "cluster_removed", true, nil)

	return nil
}

// TestClusterConnection tests a cluster connection
func (s *KubernetesClusterService) TestClusterConnection(ctx context.Context, userID primitive.ObjectID, clusterName string) error {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		_ = s.clusterRepo.UpdateStatus(ctx, cluster.ID, models.ClusterStatusError)
		s.logConnection(ctx, cluster.ID, userID, "connection_test", false, err)
		return fmt.Errorf("failed to create connection: %w", err)
	}

	// Test connection
	_, err = clientset.Discovery().ServerVersion()

	status := models.ClusterStatusConnected
	if err != nil {
		status = models.ClusterStatusError
	}

	if updateErr := s.clusterRepo.UpdateStatus(ctx, cluster.ID, status); updateErr != nil {
		s.logger.WithError(updateErr).Warn("failed to update cluster status after connection test")
	}
	s.logConnection(ctx, cluster.ID, userID, "connection_test", err == nil, err)

	return err
}

// SetDefaultCluster sets a cluster as the default for a user
func (s *KubernetesClusterService) SetDefaultCluster(ctx context.Context, userID primitive.ObjectID, clusterName string) error {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	return s.clusterRepo.SetDefault(ctx, userID, cluster.ID)
}

// GetDefaultCluster gets the default cluster for a user
func (s *KubernetesClusterService) GetDefaultCluster(ctx context.Context, userID primitive.ObjectID) (*models.Cluster, error) {
	return s.clusterRepo.GetDefault(ctx, userID)
}

// UpdateClusterCredentials updates cluster credentials
func (s *KubernetesClusterService) UpdateClusterCredentials(ctx context.Context, userID primitive.ObjectID, clusterName string, credentials models.ClusterCredentials) error {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	// Only owner can update credentials
	if cluster.UserID != userID {
		return fmt.Errorf("only cluster owner can update credentials")
	}

	// Test new credentials
	cluster.Credentials = credentials
	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}

	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to validate new credentials: %w", err)
	}

	// Update in database
	return s.clusterRepo.Update(ctx, cluster.ID, bson.M{
		"credentials": credentials,
		"status":      models.ClusterStatusConnected,
	})
}

// ShareCluster shares a cluster with another user
func (s *KubernetesClusterService) ShareCluster(ctx context.Context, ownerID, clusterID, targetUserID primitive.ObjectID, role string, namespaces []string) error {
	cluster, err := s.clusterRepo.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	if cluster.UserID != ownerID {
		return fmt.Errorf("only cluster owner can share access")
	}

	access := &models.ClusterAccess{
		ClusterID:  clusterID,
		UserID:     targetUserID,
		Role:       role,
		Namespaces: namespaces,
		GrantedBy:  ownerID,
	}

	if err := s.clusterRepo.GrantAccess(ctx, access); err != nil {
		return fmt.Errorf("failed to grant access: %w", err)
	}

	s.logConnection(ctx, clusterID, ownerID, "access_granted", true, bson.M{
		"target_user": targetUserID,
		"role":        role,
	})

	return nil
}

// RevokeClusterAccess revokes a user's access to a cluster
func (s *KubernetesClusterService) RevokeClusterAccess(ctx context.Context, ownerID, clusterID, targetUserID primitive.ObjectID) error {
	cluster, err := s.clusterRepo.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	if cluster.UserID != ownerID {
		return fmt.Errorf("only cluster owner can revoke access")
	}

	return s.clusterRepo.RevokeAccess(ctx, clusterID, targetUserID)
}

// GetClusterLogs retrieves connection logs for a cluster
func (s *KubernetesClusterService) GetClusterLogs(ctx context.Context, userID primitive.ObjectID, clusterName string, limit int64) ([]*models.ClusterConnectionLog, error) {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return nil, fmt.Errorf("cluster not found: %w", err)
	}

	return s.clusterRepo.GetConnectionLogs(ctx, cluster.ID, limit)
}

// RefreshClusterMetadata updates cluster metadata
func (s *KubernetesClusterService) RefreshClusterMetadata(ctx context.Context, userID primitive.ObjectID, clusterName string) error {
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}

	return s.updateClusterMetadata(ctx, cluster, clientset)
}

// Helper functions

func (s *KubernetesClusterService) clusterToAuthConfig(cluster *models.Cluster) *k8sauth.AuthConfig {
	auth := k8sauth.NewAuthConfig(k8sauth.AuthType(cluster.AuthType))
	auth.ServerURL = cluster.Server

	switch cluster.AuthType {
	case models.ClusterAuthToken, models.ClusterAuthServiceAccount:
		auth.BearerToken = cluster.Credentials.Token
	case models.ClusterAuthCertificate:
		auth.ClientCertData = cluster.Credentials.ClientCertData
		auth.ClientKeyData = cluster.Credentials.ClientKeyData
	case models.ClusterAuthKubeConfig:
		auth.KubeConfigContent = cluster.Credentials.KubeConfig
	case models.ClusterAuthOIDC:
		auth.OIDCIssuerURL = cluster.Credentials.OIDCIssuerURL
		auth.OIDCClientID = cluster.Credentials.OIDCClientID
		auth.OIDCClientSecret = cluster.Credentials.OIDCClientSecret
		auth.OIDCRefreshToken = cluster.Credentials.OIDCRefreshToken
		auth.OIDCScopes = cluster.Credentials.OIDCScopes
	}

	auth.CAData = cluster.Credentials.CAData
	auth.Insecure = cluster.Credentials.Insecure

	return auth
}

func (s *KubernetesClusterService) updateClusterMetadata(ctx context.Context, cluster *models.Cluster, clientset *kubernetes.Clientset) error {
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warn("failed to list nodes for metadata update")
	}
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warn("failed to list namespaces for metadata update")
	}
	storageClasses, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warn("failed to list storage classes for metadata update")
	}

	metadata := models.ClusterMetadata{
		Version:   version.String(),
		NodeCount: len(nodes.Items),
	}

	if namespaces != nil {
		for _, ns := range namespaces.Items {
			metadata.Namespaces = append(metadata.Namespaces, ns.Name)
		}
	}

	if storageClasses != nil {
		for _, sc := range storageClasses.Items {
			metadata.StorageClasses = append(metadata.StorageClasses, sc.Name)
		}
	}

	if len(nodes.Items) > 0 {
		node := nodes.Items[0]
		if platform, ok := node.Labels["kubernetes.io/os"]; ok {
			metadata.Platform = platform
		}
		if provider, ok := node.Labels["kubernetes.io/cloud-provider"]; ok {
			metadata.Provider = provider
		}
	}

	cluster.Metadata = metadata
	return s.clusterRepo.UpdateMetadata(ctx, cluster.ID, metadata)
}

func (s *KubernetesClusterService) logConnection(ctx context.Context, clusterID, userID primitive.ObjectID, action string, success bool, details interface{}) {
	log := &models.ClusterConnectionLog{
		ClusterID: clusterID,
		UserID:    userID,
		Action:    action,
		Success:   success,
	}

	if !success && details != nil {
		if err, ok := details.(error); ok {
			log.Error = err.Error()
		}
	}

	if detailsMap, ok := details.(bson.M); ok {
		log.Details = detailsMap
	}

	if err := s.clusterRepo.LogConnection(ctx, log); err != nil {
		s.logger.WithError(err).Warn("Failed to log connection")
	}
}

// Singleton instance
var (
	clusterServiceInstance *KubernetesClusterService
	once                   sync.Once
)

func GetKubernetesClusterService() *KubernetesClusterService {
	once.Do(func() {
		clusterServiceInstance = NewKubernetesClusterService()
	})
	return clusterServiceInstance
}
