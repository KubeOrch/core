package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	k8sauth "github.com/KubeOrch/core/pkg/kubernetes"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type KubernetesClusterService struct {
	clusterRepo *repositories.ClusterRepository
	logger      *logrus.Logger
}

// Metrics cache with TTL
type cachedMetrics struct {
	metrics   *models.ClusterMetrics
	fetchedAt time.Time
}

var (
	metricsCache      = make(map[string]*cachedMetrics)
	metricsCacheMutex sync.RWMutex
	metricsCacheTTL   = 30 * time.Second
)

func NewKubernetesClusterService() *KubernetesClusterService {
	return &KubernetesClusterService{
		clusterRepo: repositories.NewClusterRepository(),
		logger:      logrus.New(),
	}
}

// CreateClusterConnection creates a Kubernetes client from stored cluster credentials with a 5-second timeout
func (s *KubernetesClusterService) CreateClusterConnection(cluster *models.Cluster) (*kubernetes.Clientset, error) {
	return s.createClusterConnection(cluster, 5*time.Second)
}

// CreateStreamingClusterConnection creates a Kubernetes client optimized for streaming operations (no timeout)
func (s *KubernetesClusterService) CreateStreamingClusterConnection(cluster *models.Cluster) (*kubernetes.Clientset, error) {
	return s.createClusterConnection(cluster, 0)
}

// createClusterConnection is a private helper to create a Kubernetes client with a specific timeout
func (s *KubernetesClusterService) createClusterConnection(cluster *models.Cluster, timeout time.Duration) (*kubernetes.Clientset, error) {
	auth := s.ClusterToAuthConfig(cluster)

	timeoutStr := timeout.String()
	if timeout == 0 {
		timeoutStr = "none (streaming)"
	}

	s.logger.WithFields(logrus.Fields{
		"server":    cluster.Server,
		"auth_type": cluster.AuthType,
		"timeout":   timeoutStr,
	}).Debug("Building REST config for cluster")

	config, err := auth.BuildRESTConfig()
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"server": cluster.Server,
			"error":  err.Error(),
		}).Error("Failed to build REST config")
		return nil, fmt.Errorf("failed to build REST config: %w", err)
	}

	config.Timeout = timeout
	config.QPS = 100
	config.Burst = 100

	s.logger.WithFields(logrus.Fields{
		"server":  cluster.Server,
		"timeout": timeoutStr,
	}).Debug("Creating Kubernetes clientset")

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"server": cluster.Server,
			"error":  err.Error(),
		}).Error("Failed to create Kubernetes clientset")
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// AddCluster adds a new cluster to the database
func (s *KubernetesClusterService) AddCluster(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster) error {
	cluster.UserID = userID
	cluster.Status = models.ClusterStatusUnknown

	// Save cluster first
	if err := s.clusterRepo.Create(ctx, cluster); err != nil {
		return fmt.Errorf("failed to save cluster: %w", err)
	}

	// Test connection asynchronously with a timeout
	go func() {
		testCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		s.logger.WithFields(logrus.Fields{
			"cluster_name": cluster.Name,
			"server":       cluster.Server,
			"auth_type":    cluster.AuthType,
		}).Info("Starting cluster connection test")

		clientset, err := s.CreateClusterConnection(cluster)
		if err != nil {
			cluster.Status = models.ClusterStatusError
			s.logger.WithFields(logrus.Fields{
				"cluster_name": cluster.Name,
				"server":       cluster.Server,
				"error":        err.Error(),
			}).Error("Failed to create cluster connection")
			if err := s.clusterRepo.UpdateStatus(testCtx, cluster.ID, models.ClusterStatusError); err != nil {
				s.logger.WithError(err).Error("Failed to update cluster status to error")
			}
		} else {
			s.logger.WithFields(logrus.Fields{
				"cluster_name": cluster.Name,
				"server":       cluster.Server,
			}).Info("Successfully created Kubernetes client, testing server version")
			
			// Validate connection
			version, err := clientset.Discovery().ServerVersion()
			if err != nil {
				cluster.Status = models.ClusterStatusError
				s.logger.WithFields(logrus.Fields{
					"cluster_name": cluster.Name,
					"server":       cluster.Server,
					"error":        err.Error(),
				}).Error("Failed to get server version")
				if err := s.clusterRepo.UpdateStatus(testCtx, cluster.ID, models.ClusterStatusError); err != nil {
					s.logger.WithError(err).Error("Failed to update cluster status to error after version check")
				}
			} else {
				s.logger.WithFields(logrus.Fields{
					"cluster_name": cluster.Name,
					"server":       cluster.Server,
					"version":      version.String(),
				}).Info("Successfully connected to cluster")
				
				cluster.Status = models.ClusterStatusConnected
				if err := s.clusterRepo.UpdateStatus(testCtx, cluster.ID, models.ClusterStatusConnected); err != nil {
					s.logger.WithError(err).Error("Failed to update cluster status to connected")
				}

				// Handle single-node mode if enabled
				if cluster.SingleNode {
					if err := s.manageSingleNodeTaints(testCtx, clientset, true); err != nil {
						s.logger.WithError(err).Warn("Failed to remove control-plane taints for single-node mode")
					} else {
						s.logger.Info("Successfully configured single-node mode (removed control-plane taints)")
					}
				}

				// Update metadata
				if err := s.updateClusterMetadata(testCtx, cluster, clientset); err != nil {
					s.logger.WithError(err).Warn("Failed to update cluster metadata on add")
				}
			}
		}
		s.logConnection(testCtx, cluster.ID, userID, "cluster_added", cluster.Status == models.ClusterStatusConnected, err)
	}()

	// Return immediately after saving

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

// UpdateCluster updates a cluster's configuration
func (s *KubernetesClusterService) UpdateCluster(ctx context.Context, userID primitive.ObjectID, clusterName string, updatedCluster *models.Cluster) error {
	existingCluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return fmt.Errorf("cluster not found: %w", err)
	}

	// Only owner can update cluster
	if existingCluster.UserID != userID {
		return fmt.Errorf("only cluster owner can update cluster")
	}

	// If credentials changed, test the connection
	credentialsProvided := updatedCluster.Credentials.Token != "" ||
		updatedCluster.Credentials.KubeConfig != "" ||
		(updatedCluster.Credentials.ClientCertData != "" && updatedCluster.Credentials.ClientKeyData != "") ||
		updatedCluster.Credentials.OIDCIssuerURL != ""
	if credentialsProvided {
		clientset, err := s.CreateClusterConnection(updatedCluster)
		if err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		_, err = clientset.Discovery().ServerVersion()
		if err != nil {
			return fmt.Errorf("failed to validate configuration: %w", err)
		}
	}

	// Handle single-node mode changes if needed
	if existingCluster.SingleNode != updatedCluster.SingleNode {
		// Create connection with existing cluster credentials
		connectCluster := existingCluster
		// Use new credentials if provided
		if credentialsProvided {
			connectCluster.Credentials = updatedCluster.Credentials
		}

		clientset, err := s.CreateClusterConnection(connectCluster)
		if err != nil {
			return fmt.Errorf("failed to connect to cluster for single-node mode change: %w", err)
		}

		// Manage taints for single-node mode
		if err := s.manageSingleNodeTaints(ctx, clientset, updatedCluster.SingleNode); err != nil {
			return fmt.Errorf("failed to manage single-node taints: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"cluster":     clusterName,
			"single_node": updatedCluster.SingleNode,
		}).Info("Toggled single-node mode")
	}

	// Prepare update fields
	updateFields := bson.M{
		"display_name": updatedCluster.DisplayName,
		"description":  updatedCluster.Description,
		"server":       updatedCluster.Server,
		"auth_type":    updatedCluster.AuthType,
		"labels":       updatedCluster.Labels,
		"single_node":  updatedCluster.SingleNode,
		"updated_at":   time.Now(),
	}

	// Only update credentials if they were provided
	if credentialsProvided {
		updateFields["credentials"] = updatedCluster.Credentials
		updateFields["status"] = models.ClusterStatusConnected
	}

	// Update in database
	return s.clusterRepo.Update(ctx, existingCluster.ID, updateFields)
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

func (s *KubernetesClusterService) ClusterToAuthConfig(cluster *models.Cluster) *k8sauth.AuthConfig {
	auth := k8sauth.NewAuthConfig(cluster.AuthType)
	auth.ServerURL = cluster.Server

	switch cluster.AuthType {
	case models.ClusterAuthToken:
		auth.BearerToken = cluster.Credentials.Token
	case models.ClusterAuthServiceAccount:
		auth.ServiceAccountToken = cluster.Credentials.Token
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
		Version:     version.String(),
		NodeCount:   len(nodes.Items),
		LastUpdated: time.Now(),
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

// manageSingleNodeTaints manages control-plane taints for single-node clusters
func (s *KubernetesClusterService) manageSingleNodeTaints(ctx context.Context, clientset *kubernetes.Clientset, remove bool) error {
	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	controlPlaneTaintKey := "node-role.kubernetes.io/control-plane"
	masterTaintKey := "node-role.kubernetes.io/master" // For older clusters

	for _, node := range nodes.Items {
		updated := false
		newTaints := []corev1.Taint{}

		// Process existing taints
		for _, taint := range node.Spec.Taints {
			// Skip control-plane taints if we're removing them
			if remove && (taint.Key == controlPlaneTaintKey || taint.Key == masterTaintKey) {
				s.logger.WithFields(logrus.Fields{
					"node":  node.Name,
					"taint": taint.Key,
				}).Info("Removing control-plane taint for single-node mode")
				updated = true
				continue
			}
			newTaints = append(newTaints, taint)
		}

		// Add control-plane taint back if we're disabling single-node mode
		if !remove {
			hasTaint := false
			for _, taint := range node.Spec.Taints {
				if taint.Key == controlPlaneTaintKey {
					hasTaint = true
					break
				}
			}
			if !hasTaint && s.isControlPlaneNode(&node) {
				newTaints = append(newTaints, corev1.Taint{
					Key:    controlPlaneTaintKey,
					Effect: corev1.TaintEffectNoSchedule,
				})
				s.logger.WithFields(logrus.Fields{
					"node": node.Name,
				}).Info("Adding control-plane taint back (single-node mode disabled)")
				updated = true
			}
		}

		// Update node if taints changed using JSON Patch for better conflict handling
		if updated {
			// Create JSON patch for taints
			patchData, err := json.Marshal([]map[string]interface{}{
				{
					"op":    "replace",
					"path":  "/spec/taints",
					"value": newTaints,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to create patch for node %s: %w", node.Name, err)
			}

			// Apply the patch
			if _, err := clientset.CoreV1().Nodes().Patch(ctx, node.Name, types.JSONPatchType, patchData, metav1.PatchOptions{}); err != nil {
				return fmt.Errorf("failed to patch node %s: %w", node.Name, err)
			}
		}
	}

	return nil
}

// isControlPlaneNode checks if a node is a control-plane node
func (s *KubernetesClusterService) isControlPlaneNode(node *corev1.Node) bool {
	controlPlaneLabels := []string{
		"node-role.kubernetes.io/control-plane",
		"node-role.kubernetes.io/master",
	}

	for _, label := range controlPlaneLabels {
		if _, exists := node.Labels[label]; exists {
			return true
		}
	}
	return false
}

// ToggleSingleNodeMode toggles single-node mode for a cluster
func (s *KubernetesClusterService) ToggleSingleNodeMode(ctx context.Context, userID primitive.ObjectID, clusterName string, enable bool) error {
	// Get cluster
	cluster, err := s.GetClusterByName(ctx, userID, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	// Skip if already in the desired state
	if cluster.SingleNode == enable {
		return nil
	}

	// Create connection
	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Manage taints
	if err := s.manageSingleNodeTaints(ctx, clientset, enable); err != nil {
		return fmt.Errorf("failed to manage taints: %w", err)
	}

	// Update cluster in database
	updateData := bson.M{
		"single_node": enable,
	}
	if err := s.clusterRepo.Update(ctx, cluster.ID, updateData); err != nil {
		return fmt.Errorf("failed to update cluster: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"cluster":     clusterName,
		"single_node": enable,
	}).Info("Toggled single-node mode")

	return nil
}

// GetClusterMetrics fetches real-time cluster metrics with caching
func (s *KubernetesClusterService) GetClusterMetrics(ctx context.Context, userID primitive.ObjectID, clusterName string) (*models.ClusterMetrics, error) {
	// Build cache key from user and cluster
	cacheKey := fmt.Sprintf("%s:%s", userID.Hex(), clusterName)

	// Check cache first
	metricsCacheMutex.RLock()
	if cached, exists := metricsCache[cacheKey]; exists && time.Since(cached.fetchedAt) < metricsCacheTTL {
		metricsCacheMutex.RUnlock()
		s.logger.WithField("cluster", clusterName).Debug("Returning cached metrics")
		return cached.metrics, nil
	}
	metricsCacheMutex.RUnlock()

	// Get cluster and create connection
	cluster, err := s.clusterRepo.GetByName(ctx, clusterName, userID)
	if err != nil {
		return nil, fmt.Errorf("cluster not found: %w", err)
	}

	clientset, err := s.CreateClusterConnection(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Fetch metrics in parallel
	var wg sync.WaitGroup
	var health []models.ComponentHealth
	var resources models.ResourceUsage
	var nodeCount, podCount int
	var healthErr, resourcesErr, nodesErr, podsErr error

	wg.Add(4)

	// Fetch component health
	go func() {
		defer wg.Done()
		health, healthErr = s.fetchComponentHealth(ctx, clientset)
	}()

	// Fetch resource usage
	go func() {
		defer wg.Done()
		resources, resourcesErr = s.fetchResourceUsage(ctx, clientset)
	}()

	// Fetch node count
	go func() {
		defer wg.Done()
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			nodesErr = err
			return
		}
		nodeCount = len(nodes.Items)
	}()

	// Fetch pod count
	go func() {
		defer wg.Done()
		pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if err != nil {
			podsErr = err
			return
		}
		podCount = len(pods.Items)
	}()

	wg.Wait()

	// Log any errors but continue with partial data
	if healthErr != nil {
		s.logger.WithError(healthErr).Warn("Failed to fetch component health")
	}
	if resourcesErr != nil {
		s.logger.WithError(resourcesErr).Warn("Failed to fetch resource usage")
	}
	if nodesErr != nil {
		s.logger.WithError(nodesErr).Warn("Failed to fetch node count")
	}
	if podsErr != nil {
		s.logger.WithError(podsErr).Warn("Failed to fetch pod count")
	}

	metrics := &models.ClusterMetrics{
		ClusterName: clusterName,
		Health:      health,
		Resources:   resources,
		NodeCount:   nodeCount,
		PodCount:    podCount,
		LastUpdated: time.Now(),
	}

	// Update cache
	metricsCacheMutex.Lock()
	metricsCache[cacheKey] = &cachedMetrics{
		metrics:   metrics,
		fetchedAt: time.Now(),
	}
	metricsCacheMutex.Unlock()

	return metrics, nil
}

// fetchComponentHealth gets health status of Kubernetes control plane components
func (s *KubernetesClusterService) fetchComponentHealth(ctx context.Context, clientset *kubernetes.Clientset) ([]models.ComponentHealth, error) {
	health := []models.ComponentHealth{}

	// API Server is healthy if we can make requests (we already have a connection)
	health = append(health, models.ComponentHealth{
		Name:   "API Server",
		Status: models.ComponentHealthy,
	})

	// Try to get ComponentStatus (deprecated in 1.19+ but still works in many clusters)
	componentStatuses, err := clientset.CoreV1().ComponentStatuses().List(ctx, metav1.ListOptions{})
	if err == nil && len(componentStatuses.Items) > 0 {
		for _, cs := range componentStatuses.Items {
			var status models.ComponentHealthStatus
			var message string

			for _, condition := range cs.Conditions {
				if condition.Type == corev1.ComponentHealthy {
					if condition.Status == corev1.ConditionTrue {
						status = models.ComponentHealthy
					} else {
						status = models.ComponentUnhealthy
						message = condition.Message
					}
					break
				}
			}

			if status == "" {
				status = models.ComponentUnknown
			}

			// Map component names to display names
			displayName := cs.Name
			switch cs.Name {
			case "scheduler":
				displayName = "Scheduler"
			case "controller-manager":
				displayName = "Controller"
			case "etcd-0", "etcd-1", "etcd-2":
				displayName = "etcd"
			}

			// Skip if we already have this component (e.g., multiple etcd instances)
			found := false
			for _, h := range health {
				if h.Name == displayName {
					found = true
					break
				}
			}
			if !found {
				health = append(health, models.ComponentHealth{
					Name:    displayName,
					Status:  status,
					Message: message,
				})
			}
		}
	} else {
		// ComponentStatus not available, try health endpoints via raw REST client
		// Check scheduler
		schedulerHealth := s.checkHealthEndpoint(ctx, clientset, "/livez")
		if schedulerHealth {
			health = append(health, models.ComponentHealth{
				Name:   "Scheduler",
				Status: models.ComponentHealthy,
			})
		} else {
			health = append(health, models.ComponentHealth{
				Name:   "Scheduler",
				Status: models.ComponentUnknown,
			})
		}

		// Add controller-manager and etcd as unknown since we can't check them directly
		health = append(health, models.ComponentHealth{
			Name:   "Controller",
			Status: models.ComponentUnknown,
		})
		health = append(health, models.ComponentHealth{
			Name:   "etcd",
			Status: models.ComponentUnknown,
		})
	}

	return health, nil
}

// checkHealthEndpoint checks a health endpoint via the K8s API
func (s *KubernetesClusterService) checkHealthEndpoint(ctx context.Context, clientset *kubernetes.Clientset, path string) bool {
	result := clientset.Discovery().RESTClient().Get().AbsPath(path).Do(ctx)
	return result.Error() == nil
}

// fetchResourceUsage gets CPU, Memory, and Storage usage across all nodes
func (s *KubernetesClusterService) fetchResourceUsage(ctx context.Context, clientset *kubernetes.Clientset) (models.ResourceUsage, error) {
	usage := models.ResourceUsage{}

	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return usage, fmt.Errorf("failed to list nodes: %w", err)
	}

	var totalCPUCapacity, totalMemoryCapacity, totalStorageCapacity int64
	var totalCPUUsed, totalMemoryUsed, totalStorageUsed int64

	// Aggregate capacity from all nodes
	for _, node := range nodes.Items {
		if cpu := node.Status.Capacity.Cpu(); cpu != nil {
			totalCPUCapacity += cpu.MilliValue()
		}
		if memory := node.Status.Capacity.Memory(); memory != nil {
			totalMemoryCapacity += memory.Value()
		}
		if storage := node.Status.Capacity.StorageEphemeral(); storage != nil {
			totalStorageCapacity += storage.Value()
		}
	}

	// Try to get actual usage from metrics-server
	metricsAvailable := s.fetchNodeMetrics(ctx, clientset, &totalCPUUsed, &totalMemoryUsed)

	if !metricsAvailable {
		// Fallback: estimate usage from allocatable vs capacity
		for _, node := range nodes.Items {
			if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
				allocatableCPU := cpu.MilliValue()
				capacityCPU := node.Status.Capacity.Cpu().MilliValue()
				// Rough estimate: system reserved = capacity - allocatable
				totalCPUUsed += (capacityCPU - allocatableCPU)
			}
			if memory := node.Status.Allocatable.Memory(); memory != nil {
				allocatableMemory := memory.Value()
				capacityMemory := node.Status.Capacity.Memory().Value()
				totalMemoryUsed += (capacityMemory - allocatableMemory)
			}
		}
	}

	// Get storage usage from PVCs
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, pvc := range pvcs.Items {
			if storage := pvc.Status.Capacity.Storage(); storage != nil {
				totalStorageUsed += storage.Value()
			}
		}
	}

	// Calculate percentages
	usage.CPU = models.ResourceMetric{
		Used:       totalCPUUsed,
		Capacity:   totalCPUCapacity,
		Percentage: calculatePercentage(totalCPUUsed, totalCPUCapacity),
	}
	usage.Memory = models.ResourceMetric{
		Used:       totalMemoryUsed,
		Capacity:   totalMemoryCapacity,
		Percentage: calculatePercentage(totalMemoryUsed, totalMemoryCapacity),
	}
	usage.Storage = models.ResourceMetric{
		Used:       totalStorageUsed,
		Capacity:   totalStorageCapacity,
		Percentage: calculatePercentage(totalStorageUsed, totalStorageCapacity),
	}

	return usage, nil
}

// fetchNodeMetrics tries to fetch node metrics from metrics-server
func (s *KubernetesClusterService) fetchNodeMetrics(ctx context.Context, clientset *kubernetes.Clientset, cpuUsed, memoryUsed *int64) bool {
	// Use the metrics API path directly
	result := clientset.RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/nodes").
		Do(ctx)

	if result.Error() != nil {
		s.logger.WithError(result.Error()).Debug("Metrics server not available")
		return false
	}

	raw, err := result.Raw()
	if err != nil {
		s.logger.WithError(err).Debug("Failed to get raw metrics response")
		return false
	}

	// Parse the metrics response
	var nodeMetricsList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"items"`
	}

	if err := json.Unmarshal(raw, &nodeMetricsList); err != nil {
		s.logger.WithError(err).Debug("Failed to parse metrics response")
		return false
	}

	for _, node := range nodeMetricsList.Items {
		// Parse CPU (format: "123m" or "123456n")
		cpuStr := node.Usage.CPU
		if cpuStr != "" {
			var cpuValue int64
			if cpuStr[len(cpuStr)-1] == 'n' {
				// Nanocores to millicores
				_, _ = fmt.Sscanf(cpuStr, "%dn", &cpuValue)
				cpuValue = cpuValue / 1000000
			} else if cpuStr[len(cpuStr)-1] == 'm' {
				_, _ = fmt.Sscanf(cpuStr, "%dm", &cpuValue)
			} else {
				// Cores to millicores
				var cores float64
				_, _ = fmt.Sscanf(cpuStr, "%f", &cores)
				cpuValue = int64(cores * 1000)
			}
			*cpuUsed += cpuValue
		}

		// Parse Memory (format: "123Ki" or "123456")
		memStr := node.Usage.Memory
		if memStr != "" {
			var memValue int64
			if len(memStr) >= 2 {
				suffix := memStr[len(memStr)-2:]
				switch suffix {
				case "Ki":
					_, _ = fmt.Sscanf(memStr, "%dKi", &memValue)
					memValue *= 1024
				case "Mi":
					_, _ = fmt.Sscanf(memStr, "%dMi", &memValue)
					memValue *= 1024 * 1024
				case "Gi":
					_, _ = fmt.Sscanf(memStr, "%dGi", &memValue)
					memValue *= 1024 * 1024 * 1024
				default:
					_, _ = fmt.Sscanf(memStr, "%d", &memValue)
				}
			} else {
				_, _ = fmt.Sscanf(memStr, "%d", &memValue)
			}
			*memoryUsed += memValue
		}
	}

	return true
}

// calculatePercentage calculates a percentage, handling zero capacity
func calculatePercentage(used, capacity int64) float64 {
	if capacity == 0 {
		return 0
	}
	return float64(used) / float64(capacity) * 100
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
