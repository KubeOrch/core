package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// PodLogStreamManager manages K8s pod log streams and publishes to the unified broadcaster
type PodLogStreamManager struct {
	activeStreams map[string]*logStream // resource_id -> stream info
	mu            sync.RWMutex
	broadcaster   *SSEBroadcaster
	logger        *logrus.Entry
}

type logStream struct {
	resourceID string
	cancel     context.CancelFunc
	streamCtx  context.Context
}

var (
	logStreamManagerInstance *PodLogStreamManager
	logStreamManagerOnce     sync.Once
)

// GetPodLogStreamManager returns the singleton log stream manager instance
func GetPodLogStreamManager() *PodLogStreamManager {
	logStreamManagerOnce.Do(func() {
		logStreamManagerInstance = &PodLogStreamManager{
			activeStreams: make(map[string]*logStream),
			broadcaster:   GetSSEBroadcaster(),
			logger:        logrus.WithField("component", "pod-log-stream-manager"),
		}
		logStreamManagerInstance.logger.Info("Pod log stream manager initialized")
	})
	return logStreamManagerInstance
}

// StartLogStream starts streaming logs from a pod if not already streaming
// Returns true if a new stream was started, false if already streaming
func (m *PodLogStreamManager) StartLogStream(
	resourceID string,
	clientset *kubernetes.Clientset,
	namespace string,
	podName string,
	containerName string,
	options *corev1.PodLogOptions,
) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	streamKey := fmt.Sprintf("pod-logs:%s", resourceID)

	// Check if stream already active
	if _, exists := m.activeStreams[resourceID]; exists {
		m.logger.Debugf("Log stream already active for %s", resourceID)
		return false, nil
	}

	// Create cancellable context for the stream
	streamCtx, cancel := context.WithCancel(context.Background())

	// Store stream info
	m.activeStreams[resourceID] = &logStream{
		resourceID: resourceID,
		cancel:     cancel,
		streamCtx:  streamCtx,
	}

	m.logger.Infof("Starting K8s log stream for %s (pod: %s, container: %s)", resourceID, podName, containerName)

	// Start log streaming in background
	go m.streamLogs(streamCtx, streamKey, clientset, namespace, podName, containerName, options)

	return true, nil
}

// StopLogStream stops streaming logs for a resource
func (m *PodLogStreamManager) StopLogStream(resourceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stream, exists := m.activeStreams[resourceID]; exists {
		m.logger.Infof("Stopping K8s log stream for %s", resourceID)
		stream.cancel()
		delete(m.activeStreams, resourceID)
	}
}

// IsStreaming returns true if logs are currently streaming for a resource
func (m *PodLogStreamManager) IsStreaming(resourceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.activeStreams[resourceID]
	return exists
}

// streamLogs reads from K8s log stream and publishes to broadcaster
func (m *PodLogStreamManager) streamLogs(
	ctx context.Context,
	streamKey string,
	clientset *kubernetes.Clientset,
	namespace string,
	podName string,
	containerName string,
	options *corev1.PodLogOptions,
) {
	defer func() {
		// Cleanup when stream ends
		resourceID := streamKey[len("pod-logs:"):]
		m.StopLogStream(resourceID)

		// Send completion event
		m.broadcaster.Publish(StreamEvent{
			Type:      "pod-logs",
			StreamKey: streamKey,
			EventType: "complete",
			Data: map[string]interface{}{
				"message": "Log stream completed",
			},
		})
	}()

	// Get log stream from K8s API
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, options)
	stream, err := req.Stream(ctx)
	if err != nil {
		m.logger.WithError(err).Errorf("Failed to open K8s log stream for %s", streamKey)
		m.broadcaster.Publish(StreamEvent{
			Type:      "pod-logs",
			StreamKey: streamKey,
			EventType: "error",
			Data: map[string]interface{}{
				"message": fmt.Sprintf("Failed to stream logs: %v", err),
			},
		})
		return
	}
	defer func() { _ = stream.Close() }()

	// Read and publish logs line by line
	scanner := bufio.NewScanner(stream)
	for {
		select {
		case <-ctx.Done():
			m.logger.Infof("Log stream cancelled for %s", streamKey)
			return
		default:
			if !scanner.Scan() {
				// End of stream or error
				if err := scanner.Err(); err != nil && err != io.EOF {
					m.logger.WithError(err).Warnf("Log stream error for %s", streamKey)
					m.broadcaster.Publish(StreamEvent{
						Type:      "pod-logs",
						StreamKey: streamKey,
						EventType: "error",
						Data: map[string]interface{}{
							"message": fmt.Sprintf("Stream error: %v", err),
						},
					})
				}
				return
			}

			line := scanner.Text()

			// Publish log line to broadcaster
			m.broadcaster.Publish(StreamEvent{
				Type:      "pod-logs",
				StreamKey: streamKey,
				EventType: "log",
				Data: map[string]interface{}{
					"line": line,
				},
			})
		}
	}
}

// CheckAndCleanupIdleStreams stops streams that have no subscribers
func (m *PodLogStreamManager) CheckAndCleanupIdleStreams() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for resourceID := range m.activeStreams {
		streamKey := fmt.Sprintf("pod-logs:%s", resourceID)
		subscriberCount := m.broadcaster.GetSubscriberCount(streamKey)

		if subscriberCount == 0 {
			m.logger.Infof("No subscribers for %s, stopping stream", streamKey)
			if stream, exists := m.activeStreams[resourceID]; exists {
				stream.cancel()
				delete(m.activeStreams, resourceID)
			}
		}
	}
}

// StartCleanupTask starts a background task that periodically checks for idle streams
func (m *PodLogStreamManager) StartCleanupTask(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			m.CheckAndCleanupIdleStreams()
		}
	}()
}
