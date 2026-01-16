package services

import (
	"context"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const metadataStalenessThreshold = 10 * time.Minute

type ClusterHealthMonitor struct {
	clusterService *KubernetesClusterService
	logger         *logrus.Logger
	interval       time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
	mu             sync.RWMutex
	running        bool
}

func NewClusterHealthMonitor(interval time.Duration) *ClusterHealthMonitor {
	if interval == 0 {
		interval = 60 * time.Second // Default to 60 seconds
	}

	return &ClusterHealthMonitor{
		clusterService: GetKubernetesClusterService(),
		logger:         logrus.New(),
		interval:       interval,
		stopChan:       make(chan struct{}),
	}
}

// Start begins the health monitoring routine
func (m *ClusterHealthMonitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.monitorLoop()
	m.logger.Infof("Cluster health monitor started with interval: %v", m.interval)
}

// Stop gracefully stops the health monitoring
func (m *ClusterHealthMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopChan)
	m.wg.Wait()
	m.logger.Info("Cluster health monitor stopped")
}

func (m *ClusterHealthMonitor) monitorLoop() {
	defer m.wg.Done()

	// Run immediately on start
	m.checkAllClusters()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAllClusters()
		case <-m.stopChan:
			return
		}
	}
}

func (m *ClusterHealthMonitor) checkAllClusters() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all clusters from the repository
	clusters, err := m.clusterService.clusterRepo.GetAll(ctx)
	if err != nil {
		m.logger.WithError(err).Error("Failed to fetch clusters for health check")
		return
	}

	m.logger.Debugf("Checking health status for %d clusters", len(clusters))

	// Check each cluster concurrently with a worker pool
	workerPool := make(chan struct{}, 5) // Limit to 5 concurrent checks
	var wg sync.WaitGroup

	for _, cluster := range clusters {
		wg.Add(1)
		workerPool <- struct{}{} // Acquire worker slot

		go func(c *models.Cluster) {
			defer func() {
				<-workerPool // Release worker slot
				wg.Done()
			}()

			m.checkClusterHealth(ctx, c)
		}(cluster)
	}

	wg.Wait()
}

func (m *ClusterHealthMonitor) checkClusterHealth(ctx context.Context, cluster *models.Cluster) {
	// Skip if cluster was checked recently (within 30 seconds to avoid too frequent updates)
	if time.Since(cluster.LastCheck) < 30*time.Second {
		return
	}

	previousStatus := cluster.Status
	var newStatus models.ClusterStatus

	// Try to connect to the cluster
	clientset, err := m.clusterService.CreateClusterConnection(cluster)
	if err != nil {
		newStatus = models.ClusterStatusError
		m.logger.WithError(err).Debugf("Failed to create connection for cluster %s", cluster.Name)
	} else {
		// Test the connection by getting the server version
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_ = timeoutCtx // Use the context (not needed for Discovery().ServerVersion() but keeping for future use)
		_, err = clientset.Discovery().ServerVersion()
		if err != nil {
			newStatus = models.ClusterStatusDisconnected
			m.logger.WithError(err).Debugf("Failed to reach cluster %s", cluster.Name)
		} else {
			newStatus = models.ClusterStatusConnected

			// Refresh metadata if empty or stale
			metadataStale := cluster.Metadata.Version == "" || time.Since(cluster.Metadata.LastUpdated) > metadataStalenessThreshold
			if metadataStale {
				if err := m.clusterService.updateClusterMetadata(ctx, cluster, clientset); err != nil {
					m.logger.WithError(err).Debugf("Failed to refresh metadata for cluster %s", cluster.Name)
				} else {
					m.logger.Debugf("Refreshed metadata for cluster %s (version: %s)", cluster.Name, cluster.Metadata.Version)
				}
			}
		}
	}

	// Only update if status changed to avoid unnecessary DB writes
	if previousStatus != newStatus {
		if err := m.clusterService.clusterRepo.UpdateStatus(ctx, cluster.ID, newStatus); err != nil {
			m.logger.WithError(err).Errorf("Failed to update status for cluster %s", cluster.Name)
		} else {
			m.logger.Infof("Cluster %s status changed from %s to %s", cluster.Name, previousStatus, newStatus)

			// Log the status change
			m.clusterService.logConnection(ctx, cluster.ID, cluster.UserID, "health_check",
				newStatus == models.ClusterStatusConnected, nil)
		}
	} else {
		// Even if status didn't change, update the last_check timestamp
		// This is a lightweight update to track that we're still monitoring
		if err := m.clusterService.clusterRepo.UpdateLastCheck(ctx, cluster.ID); err != nil {
			m.logger.WithError(err).Debugf("Failed to update last_check for cluster %s", cluster.Name)
		}
	}
}

// CheckSingleCluster allows manual health check of a specific cluster
func (m *ClusterHealthMonitor) CheckSingleCluster(ctx context.Context, clusterID primitive.ObjectID) error {
	cluster, err := m.clusterService.clusterRepo.GetByID(ctx, clusterID)
	if err != nil {
		return err
	}

	m.checkClusterHealth(ctx, cluster)
	return nil
}

// GetHealthStatus returns the current health status without checking
func (m *ClusterHealthMonitor) GetHealthStatus(ctx context.Context, clusterID primitive.ObjectID) (models.ClusterStatus, error) {
	cluster, err := m.clusterService.clusterRepo.GetByID(ctx, clusterID)
	if err != nil {
		return models.ClusterStatusUnknown, err
	}

	// Consider status stale if not checked in the last 2 minutes
	if time.Since(cluster.LastCheck) > 2*time.Minute {
		return models.ClusterStatusUnknown, nil
	}

	return cluster.Status, nil
}

// Singleton instance
var (
	healthMonitorInstance *ClusterHealthMonitor
	healthMonitorOnce     sync.Once
)

// GetClusterHealthMonitor returns the singleton instance
func GetClusterHealthMonitor() *ClusterHealthMonitor {
	healthMonitorOnce.Do(func() {
		healthMonitorInstance = NewClusterHealthMonitor(60 * time.Second)
	})
	return healthMonitorInstance
}