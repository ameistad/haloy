package logging

import (
    "context"
    "log/slog"
)

// StreamPublisher defines the interface for publishing log entries
type StreamPublisher interface {
    Publish(deploymentID string, entry LogEntry)
}

// LogEntry represents a log entry for streaming
type LogEntry struct {
    Level      string `json:"level"`
    Message    string `json:"message"`
    IsComplete bool   `json:"complete,omitempty"`
}

// StreamHandler ships every slog.Record to a StreamPublisher
// and then (optionally) delegates to another handler (console/file etc.).
type StreamHandler struct {
    publisher StreamPublisher
    next      slog.Handler
}

// NewStreamHandler creates a new StreamHandler
func NewStreamHandler(publisher StreamPublisher, next slog.Handler) *StreamHandler {
    return &StreamHandler{publisher: publisher, next: next}
}

func (h *StreamHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
    if h.next != nil {
        return h.next.Enabled(ctx, lvl)
    }
    return true
}

func (h *StreamHandler) Handle(ctx context.Context, rec slog.Record) error {
    // Extract deployment-related attributes
    var depID string
    var isComplete bool

    rec.Attrs(func(a slog.Attr) bool {
        switch a.Key {
        case "deploymentID":
            depID = a.Value.String()
        case "deployment_complete", "complete":
            isComplete = a.Value.Bool()
        }
        return true
    })

    // Push to publisher if we have a deployment ID
    if depID != "" && h.publisher != nil {
        entry := LogEntry{
            Level:      h.mapLogLevel(rec.Level),
            Message:    rec.Message,
            IsComplete: isComplete,
        }
        h.publisher.Publish(depID, entry)
    }

    // Delegate to next handler
    if h.next != nil {
        return h.next.Handle(ctx, rec)
    }
    return nil
}

func (h *StreamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    next := h.next
    if next != nil {
        next = next.WithAttrs(attrs)
    }
    return &StreamHandler{publisher: h.publisher, next: next}
}

func (h *StreamHandler) WithGroup(name string) slog.Handler {
    next := h.next
    if next != nil {
        next = next.WithGroup(name)
    }
    return &StreamHandler{publisher: h.publisher, next: next}
}

func (h *StreamHandler) mapLogLevel(level slog.Level) string {
    switch {
    case level >= slog.LevelError:
        return "error"
    case level >= slog.LevelWarn:
        return "warn"
    case level >= slog.LevelInfo:
        return "info"
    default:
        return "debug"
    }
}
