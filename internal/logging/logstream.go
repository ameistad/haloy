package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
func (h *StreamHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Extract deployment ID and other fields
	var deploymentID string
	var isComplete, isFailed bool
	fields := make(map[string]interface{})

	// First, process persistent attributes from With() calls
	for _, attr := range h.persistentAttrs { // Change sh to h
		switch attr.Key {
		case "deploymentID":
			deploymentID = attr.Value.String()
		case "complete":
			isComplete = attr.Value.Bool()
		case "failed":
			isFailed = attr.Value.Bool()
		default:
			fields[attr.Key] = attr.Value.Any()
		}
	}

	// Then process record attributes (these can override persistent ones)
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "deploymentID":
			deploymentID = a.Value.String()
		case "complete":
			isComplete = a.Value.Bool()
		case "failed":
			isFailed = a.Value.Bool()
		default:
			fields[a.Key] = a.Value.Any()
		}
		return true
	})

	// Publish to stream if we have a deployment ID
	if deploymentID != "" && h.publisher != nil { // Change sh to h
		entry := LogEntry{
			Level:        rec.Level.String(),
			Message:      rec.Message,
			Timestamp:    rec.Time,
			DeploymentID: deploymentID,
			IsComplete:   isComplete,
			IsFailed:     isFailed,
			Fields:       fields,
		}
		h.publisher.Publish(deploymentID, entry) // Change sh to h
	}

	// Pass to next handler (console output)
	if h.next != nil { // Change sh to h
		return h.next.Handle(ctx, rec) // Change sh to h
	}
	return nil
}

// WithAttrs creates a new handler with additional persistent attributes
func (h *StreamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Combine existing persistent attributes with new ones
	newAttrs := make([]slog.Attr, len(h.persistentAttrs)+len(attrs))
	copy(newAttrs, h.persistentAttrs)
	copy(newAttrs[len(h.persistentAttrs):], attrs)

	newHandler := &StreamHandler{
		publisher:       h.publisher,
		persistentAttrs: newAttrs,
	}

	// Also call WithAttrs on the next handler
	if h.next != nil {
		newHandler.next = h.next.WithAttrs(attrs)
	}

	return newHandler
}

// Enabled delegates to the next handler
func (h *StreamHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.next != nil {
		return h.next.Enabled(ctx, level)
	}
	return true
}

// WithGroup creates a new handler with a group
func (h *StreamHandler) WithGroup(name string) slog.Handler {
	newHandler := &StreamHandler{
		publisher:       h.publisher,
		persistentAttrs: h.persistentAttrs, // Keep persistent attrs through groups
	}

	if h.next != nil {
		newHandler.next = h.next.WithGroup(name)
	}

	return newHandler
}

// HTTPSSEWriter writes Server-Sent Events to HTTP response
type HTTPSSEWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

// NewHTTPSSEWriter creates a new SSE writer
func NewHTTPSSEWriter(w http.ResponseWriter) *HTTPSSEWriter {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, _ := w.(http.Flusher)
	return &HTTPSSEWriter{
		writer:  w,
		flusher: flusher,
	}
}

// WriteSSE writes a log entry as Server-Sent Event
func (w *HTTPSSEWriter) WriteSSE(entry LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		// If marshaling fails, send an error event to the client
		// This is important for the client to know that something went wrong
		// and the log stream might be interrupted.
		fmt.Fprintf(w.writer, "event: error\ndata: %s\n\n", `{"message": "failed to marshal log entry"}`)
		if w.flusher != nil {
			w.flusher.Flush()
		}
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	_, err = fmt.Fprintf(w.writer, "data: %s\n\n", data)
	if err != nil {
		return fmt.Errorf("failed to write SSE data: %w", err)
	}

	if w.flusher != nil {
		w.flusher.Flush()
	}

	return nil
}
