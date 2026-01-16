package services

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// StreamEvent represents a generic SSE stream event
type StreamEvent struct {
	Type      string                 `json:"type"`       // Stream type: "workflow", "pod-logs", etc.
	StreamKey string                 `json:"stream_key"` // Unique identifier: "workflow:<id>", "pod-logs:<id>"
	EventType string                 `json:"event_type"` // Event type: "node_update", "log", "metadata", etc.
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"` // Flexible payload
}

// SSEBroadcaster manages SSE connections for all stream types (workflows, pod logs, etc.)
type SSEBroadcaster struct {
	subscribers map[string][]chan StreamEvent // stream_key → channels
	mu          sync.RWMutex
	logger      *logrus.Entry
}

var (
	broadcasterInstance *SSEBroadcaster
	broadcasterOnce     sync.Once
)

// GetSSEBroadcaster returns the singleton broadcaster instance
func GetSSEBroadcaster() *SSEBroadcaster {
	broadcasterOnce.Do(func() {
		broadcasterInstance = &SSEBroadcaster{
			subscribers: make(map[string][]chan StreamEvent),
			logger:      logrus.WithField("component", "sse-broadcaster"),
		}
		broadcasterInstance.logger.Info("Unified SSE broadcaster initialized")
	})
	return broadcasterInstance
}

// Subscribe adds a new subscriber for a specific stream with custom buffer size
// bufferSize: 10 for status updates, 100 for logs, etc.
func (b *SSEBroadcaster) Subscribe(streamKey string, bufferSize int) chan StreamEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan StreamEvent, bufferSize)
	b.subscribers[streamKey] = append(b.subscribers[streamKey], ch)
	b.logger.Infof("New subscriber added for %s (total: %d, buffer: %d)", streamKey, len(b.subscribers[streamKey]), bufferSize)

	return ch
}

// Unsubscribe removes a subscriber for a specific stream
func (b *SSEBroadcaster) Unsubscribe(streamKey string, ch chan StreamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	channels := b.subscribers[streamKey]

	// Find and remove the channel
	for i, c := range channels {
		if c == ch {
			// Remove channel from slice
			b.subscribers[streamKey] = append(channels[:i], channels[i+1:]...)
			close(ch) // Close the channel

			// If no more subscribers, remove the stream key
			if len(b.subscribers[streamKey]) == 0 {
				delete(b.subscribers, streamKey)
				b.logger.Infof("All subscribers removed for %s", streamKey)
			} else {
				b.logger.Infof("Subscriber removed for %s (remaining: %d)", streamKey, len(b.subscribers[streamKey]))
			}
			return
		}
	}
}

// Publish sends an event to all subscribers of a specific stream
// Non-blocking: if a subscriber's channel is full, the event is dropped for that subscriber
func (b *SSEBroadcaster) Publish(event StreamEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	channels := b.subscribers[event.StreamKey]

	if len(channels) == 0 {
		// No subscribers, skip
		return
	}

	// Ensure event has timestamp
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.logger.Debugf("Publishing event type=%s/%s to %d subscribers for %s",
		event.Type, event.EventType, len(channels), event.StreamKey)

	// Send to all subscribers (non-blocking)
	droppedCount := 0
	for _, ch := range channels {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel buffer full, drop event
			droppedCount++
		}
	}

	if droppedCount > 0 {
		b.logger.Warnf("Dropped event for %d slow subscribers on %s", droppedCount, event.StreamKey)
	}
}

// GetSubscriberCount returns the number of active subscribers for a stream
func (b *SSEBroadcaster) GetSubscriberCount(streamKey string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subscribers[streamKey])
}

// GetAllStreamKeys returns all active stream keys
func (b *SSEBroadcaster) GetAllStreamKeys() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	keys := make([]string, 0, len(b.subscribers))
	for key := range b.subscribers {
		keys = append(keys, key)
	}
	return keys
}

// Close closes all subscriber channels and cleans up
func (b *SSEBroadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for key, channels := range b.subscribers {
		for _, ch := range channels {
			close(ch)
		}
		delete(b.subscribers, key)
	}

	b.logger.Info("Unified SSE broadcaster closed")
}
