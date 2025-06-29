package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LogEntry represents a structured log entry for streaming
type LogEntry struct {
	Level        string         `json:"level"`
	Message      string         `json:"message"`
	Timestamp    time.Time      `json:"timestamp"`
	DeploymentID string         `json:"deploymentID,omitempty"`
	IsComplete   bool           `json:"complete,omitempty"`
	IsFailed     bool           `json:"failed,omitempty"`
	IsSuccess    bool           `json:"success,omitempty"`
	Fields       map[string]any `json:"fields,omitempty"`
}

// StreamPublisher defines the interface for publishing log entries to streams
type StreamPublisher interface {
	Publish(deploymentID string, entry LogEntry)
	Subscribe(deploymentID string) <-chan LogEntry
	Unsubscribe(deploymentID string)
	Close()
}

// LogBroker manages log streams for different deployment IDs
type LogBroker struct {
	streams map[string][]chan LogEntry
	mutex   sync.RWMutex
	closed  bool
}

// NewLogBroker creates a new log broker
func NewLogBroker() StreamPublisher {
	return &LogBroker{
		streams: make(map[string][]chan LogEntry),
	}
}

// Publish sends a log entry to all subscribers of a deployment ID
func (lb *LogBroker) Publish(deploymentID string, entry LogEntry) {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()

	if lb.closed {
		return
	}

	channels := lb.streams[deploymentID]
	for _, ch := range channels {
		select {
		case ch <- entry:
		default:
			// Skip if channel is full to prevent blocking
		}
	}
}

// Subscribe creates a new subscription for a deployment ID
func (lb *LogBroker) Subscribe(deploymentID string) <-chan LogEntry {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	ch := make(chan LogEntry, 100)

	if lb.closed {
		close(ch)
		return ch
	}

	lb.streams[deploymentID] = append(lb.streams[deploymentID], ch)
	return ch
}

func (lb *LogBroker) Unsubscribe(deploymentID string) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if channels, exists := lb.streams[deploymentID]; exists {
		for _, ch := range channels {
			close(ch)
		}
		delete(lb.streams, deploymentID)
	}
}

// Close shuts down the broker and closes all streams
func (lb *LogBroker) Close() {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	lb.closed = true
	for deploymentID, channels := range lb.streams {
		for _, ch := range channels {
			close(ch)
		}
		delete(lb.streams, deploymentID)
	}
}

// StreamHandler wraps another slog.Handler and publishes logs to streams
type StreamHandler struct {
	publisher       StreamPublisher
	next            slog.Handler
	persistentAttrs []slog.Attr
}

// NewStreamHandler creates a new streaming handler
func NewStreamHandler(publisher StreamPublisher, next slog.Handler) slog.Handler {
	return &StreamHandler{
		publisher:       publisher,
		next:            next,
		persistentAttrs: []slog.Attr{},
	}
}

// Handle processes log records and publishes them to streams
func (sh *StreamHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Extract deployment ID and other fields
	var deploymentID string
	var isComplete, isFailed, isSuccess bool
	fields := make(map[string]any)

	// Process persistent attributes from With() calls
	for _, attr := range sh.persistentAttrs {
		switch attr.Key {
		case "deploymentID":
			deploymentID = attr.Value.String()
		case "complete":
			isComplete = attr.Value.Bool()
		case "failed":
			isFailed = attr.Value.Bool()
		case "success":
			isSuccess = attr.Value.Bool()
		case "error":
			if err, ok := attr.Value.Any().(error); ok {
				fields[attr.Key] = err.Error()
			} else {
				fields[attr.Key] = attr.Value.String()
			}
		default:
			fields[attr.Key] = attr.Value.String()
		}
	}

	// Process record attributes (these can override persistent ones)
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "deploymentID":
			deploymentID = a.Value.String()
		case "complete":
			isComplete = a.Value.Bool()
		case "failed":
			isFailed = a.Value.Bool()
		case "success":
			isSuccess = a.Value.Bool()
		case "error":
			if err, ok := a.Value.Any().(error); ok {
				fields[a.Key] = err.Error()
			} else {
				fields[a.Key] = a.Value.String()
			}
		default:
			fields[a.Key] = a.Value.String()
		}
		return true
	})

	// Publish to stream if we have a deployment ID
	if deploymentID != "" && sh.publisher != nil {
		entry := LogEntry{
			Level:        rec.Level.String(),
			Message:      rec.Message,
			Timestamp:    rec.Time,
			DeploymentID: deploymentID,
			IsComplete:   isComplete,
			IsFailed:     isFailed,
			IsSuccess:    isSuccess,
			Fields:       fields,
		}
		sh.publisher.Publish(deploymentID, entry)
	}

	// Pass to next handler (console output)
	if sh.next != nil {
		return sh.next.Handle(ctx, rec)
	}
	return nil
}

// WithAttrs creates a new handler with additional persistent attributes
func (sh *StreamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Combine existing persistent attributes with new ones
	newAttrs := make([]slog.Attr, len(sh.persistentAttrs)+len(attrs))
	copy(newAttrs, sh.persistentAttrs)
	copy(newAttrs[len(sh.persistentAttrs):], attrs)

	newHandler := &StreamHandler{
		publisher:       sh.publisher,
		persistentAttrs: newAttrs,
	}

	// Also call WithAttrs on the next handler
	if sh.next != nil {
		newHandler.next = sh.next.WithAttrs(attrs)
	}

	return newHandler
}

// Enabled delegates to the next handler
func (sh *StreamHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if sh.next != nil {
		return sh.next.Enabled(ctx, level)
	}
	return true
}

// WithGroup creates a new handler with a group
func (sh *StreamHandler) WithGroup(name string) slog.Handler {
	newHandler := &StreamHandler{
		publisher:       sh.publisher,
		persistentAttrs: sh.persistentAttrs, // Keep persistent attrs through groups
	}

	if sh.next != nil {
		newHandler.next = sh.next.WithGroup(name)
	}

	return newHandler
}
