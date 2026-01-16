package services

import (
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"k8s.io/client-go/rest"
)

// ResourceWatcherManager manages all active resource watchers (generic)
type ResourceWatcherManager struct {
	watchers map[string]*ResourceWatcher // Key: "workflow_id:node_id"
	mu       sync.RWMutex
	logger   *logrus.Entry
}

var (
	resourceWatcherManagerInstance *ResourceWatcherManager
	resourceWatcherManagerOnce     sync.Once
)

// GetResourceWatcherManager returns the singleton instance
func GetResourceWatcherManager() *ResourceWatcherManager {
	resourceWatcherManagerOnce.Do(func() {
		resourceWatcherManagerInstance = &ResourceWatcherManager{
			watchers: make(map[string]*ResourceWatcher),
			logger:   logrus.WithField("component", "resource-watcher-manager"),
		}
		resourceWatcherManagerInstance.logger.Info("Resource watcher manager initialized")
	})
	return resourceWatcherManagerInstance
}

// StartWatcher starts watching any resource type
func (m *ResourceWatcherManager) StartWatcher(
	workflowID primitive.ObjectID,
	nodeID string,
	resourceName string,
	namespace string,
	resourceType string, // "deployment", "service", "statefulset", etc.
	restConfig *rest.Config,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.makeKey(workflowID, nodeID)

	// Check if already watching
	if watcher, exists := m.watchers[key]; exists {
		if watcher.IsRunning() {
			m.logger.WithField("key", key).Debug("Already watching resource, broadcasting current status")
			// Broadcast current status for new subscribers
			if watcher.lastStatus != nil {
				watcher.broadcastStatusUpdate(watcher.lastStatus)
			}
			return nil
		}
		delete(m.watchers, key)
	}

	// Create new watcher
	watcher, err := NewResourceWatcher(ResourceWatcherConfig{
		WorkflowID:   workflowID,
		NodeID:       nodeID,
		ResourceName: resourceName,
		Namespace:    namespace,
		ResourceType: resourceType,
		RestConfig:   restConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	// Start watching
	if err := watcher.Start(); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	m.watchers[key] = watcher

	m.logger.WithFields(logrus.Fields{
		"workflow_id":    workflowID.Hex(),
		"node_id":        nodeID,
		"resource_type":  resourceType,
		"resource_name":  resourceName,
		"namespace":      namespace,
		"total_watchers": len(m.watchers),
	}).Info("Started resource watcher")

	return nil
}

// StopWatcher stops watching a specific resource
func (m *ResourceWatcherManager) StopWatcher(workflowID primitive.ObjectID, nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.makeKey(workflowID, nodeID)

	watcher, exists := m.watchers[key]
	if !exists {
		return
	}

	watcher.Stop()
	delete(m.watchers, key)

	m.logger.WithFields(logrus.Fields{
		"workflow_id":        workflowID.Hex(),
		"node_id":            nodeID,
		"remaining_watchers": len(m.watchers),
	}).Info("Stopped resource watcher")
}

// StopWorkflowWatchers stops all watchers for a specific workflow
func (m *ResourceWatcherManager) StopWorkflowWatchers(workflowID primitive.ObjectID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	workflowIDHex := workflowID.Hex()
	stoppedCount := 0

	for key, watcher := range m.watchers {
		if watcher.workflowID == workflowID {
			watcher.Stop()
			delete(m.watchers, key)
			stoppedCount++
		}
	}

	if stoppedCount > 0 {
		m.logger.WithFields(logrus.Fields{
			"workflow_id":        workflowIDHex,
			"stopped_count":      stoppedCount,
			"remaining_watchers": len(m.watchers),
		}).Info("Stopped all watchers for workflow")
	}
}

// HasWatcher checks if a watcher exists for a given workflow and node
func (m *ResourceWatcherManager) HasWatcher(workflowID primitive.ObjectID, nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := m.makeKey(workflowID, nodeID)
	watcher, exists := m.watchers[key]
	return exists && watcher.IsRunning()
}

// GetActiveCount returns the number of active watchers
func (m *ResourceWatcherManager) GetActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.watchers)
}

// Shutdown stops all watchers and cleans up resources
func (m *ResourceWatcherManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Infof("Shutting down resource watcher manager (%d active watchers)", len(m.watchers))

	for key, watcher := range m.watchers {
		watcher.Stop()
		delete(m.watchers, key)
	}

	m.logger.Info("All resource watchers stopped")
}

// makeKey creates a unique key for a workflow and node
func (m *ResourceWatcherManager) makeKey(workflowID primitive.ObjectID, nodeID string) string {
	return fmt.Sprintf("%s:%s", workflowID.Hex(), nodeID)
}

// GetWatcherInfo returns debug information about active watchers
func (m *ResourceWatcherManager) GetWatcherInfo() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	watcherList := []map[string]interface{}{}
	for key, watcher := range m.watchers {
		watcherList = append(watcherList, map[string]interface{}{
			"key":           key,
			"workflow_id":   watcher.workflowID.Hex(),
			"node_id":       watcher.nodeID,
			"resource_type": watcher.resourceType,
			"resource_name": watcher.resourceName,
			"namespace":     watcher.namespace,
			"running":       watcher.IsRunning(),
		})
	}

	return map[string]interface{}{
		"total_watchers": len(m.watchers),
		"watchers":       watcherList,
	}
}
