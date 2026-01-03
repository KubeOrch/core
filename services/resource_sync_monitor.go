package services

import (
	"context"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ResourceSyncMonitor periodically syncs Kubernetes resources to the database
type ResourceSyncMonitor struct {
	interval         time.Duration
	ticker           *time.Ticker
	stopChan         chan struct{}
	running          bool
	mu               sync.Mutex
	logger           *logrus.Entry
	resourceService  *ResourceService
	clusterService   *KubernetesClusterService
	workflowExecutor *WorkflowExecutor
}

// NewResourceSyncMonitor creates a new resource sync monitor
func NewResourceSyncMonitor(interval time.Duration) *ResourceSyncMonitor {
	return &ResourceSyncMonitor{
		interval:         interval,
		stopChan:         make(chan struct{}),
		logger:           logrus.WithField("component", "resource-sync-monitor"),
		resourceService:  GetResourceService(),
		clusterService:   GetKubernetesClusterService(),
		workflowExecutor: NewWorkflowExecutor(),
	}
}

// Start begins the resource sync monitoring
func (m *ResourceSyncMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		m.logger.Warn("Resource sync monitor already running")
		return
	}

	m.ticker = time.NewTicker(m.interval)
	m.running = true

	// Do an initial sync after a short delay
	go func() {
		time.Sleep(10 * time.Second)
		m.syncAllResources()
	}()

	go m.run()
	m.logger.Infof("Resource sync monitor started (interval: %v)", m.interval)
}

// Stop stops the resource sync monitoring
func (m *ResourceSyncMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopChan)
	if m.ticker != nil {
		m.ticker.Stop()
	}
	m.running = false
	m.logger.Info("Resource sync monitor stopped")
}

func (m *ResourceSyncMonitor) run() {
	for {
		select {
		case <-m.ticker.C:
			m.syncAllResources()
		case <-m.stopChan:
			return
		}
	}
}

func (m *ResourceSyncMonitor) syncAllResources() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	m.logger.Info("Starting resource sync for all users and clusters")

	// Get all users
	users, err := GetAllUsers()
	if err != nil {
		m.logger.WithError(err).Error("Failed to get users for resource sync")
		return
	}

	syncCount := 0
	errorCount := 0

	for _, user := range users {
		userID := user.ID

		// Get user's clusters
		clusters, err := m.clusterService.ListUserClusters(ctx, userID)
		if err != nil {
			m.logger.WithError(err).Errorf("Failed to get clusters for user %s", user.Email)
			continue
		}

		// Sync each cluster
		for _, cluster := range clusters {
			if cluster.Status == models.ClusterStatusConnected {
				m.logger.Debugf("Syncing resources for cluster %s (user: %s)", cluster.Name, user.Email)

				if err := m.resourceService.SyncClusterResources(ctx, userID, cluster); err != nil {
					m.logger.WithError(err).Errorf("Failed to sync cluster %s for user %s", cluster.Name, user.Email)
					errorCount++
				} else {
					syncCount++
				}

				// Also sync workflow node statuses
				if err := m.workflowExecutor.SyncWorkflowStatuses(ctx, userID, cluster); err != nil {
					m.logger.WithError(err).Warnf("Failed to sync workflow statuses for cluster %s", cluster.Name)
				}
			}
		}
	}

	m.logger.Infof("Resource sync completed: %d successful, %d failed", syncCount, errorCount)
}

// SyncUserResources triggers an immediate sync for a specific user
func (m *ResourceSyncMonitor) SyncUserResources(userID primitive.ObjectID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		clusters, err := m.clusterService.ListUserClusters(ctx, userID)
		if err != nil {
			m.logger.WithError(err).Error("Failed to get clusters for user")
			return
		}

		for _, cluster := range clusters {
			if cluster.Status == models.ClusterStatusConnected {
				if err := m.resourceService.SyncClusterResources(ctx, userID, cluster); err != nil {
					m.logger.WithError(err).Errorf("Failed to sync cluster %s", cluster.Name)
				}
				// Also sync workflow node statuses
				if err := m.workflowExecutor.SyncWorkflowStatuses(ctx, userID, cluster); err != nil {
					m.logger.WithError(err).Warnf("Failed to sync workflow statuses for cluster %s", cluster.Name)
				}
			}
		}
	}()
}