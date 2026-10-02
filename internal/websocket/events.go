package websocket

import (
	"context"
	"fmt"
	"time"
)

// EventType represents different types of system events
type EventType string

const (
	// Lab events
	EventLabCreated    EventType = "lab.created"
	EventLabStarted    EventType = "lab.started"
	EventLabStopped    EventType = "lab.stopped"
	EventLabCompleted  EventType = "lab.completed"
	EventLabFailed     EventType = "lab.failed"

	// Session events
	EventSessionCreated    EventType = "session.created"
	EventSessionJoined     EventType = "session.joined"
	EventSessionLeft       EventType = "session.left"
	EventSessionTerminated EventType = "session.terminated"

	// Workflow events
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventTaskStarted       EventType = "task.started"
	EventTaskCompleted     EventType = "task.completed"
	EventTaskFailed        EventType = "task.failed"

	// File system events
	EventFileCreated   EventType = "file.created"
	EventFileModified  EventType = "file.modified"
	EventFileDeleted   EventType = "file.deleted"
	EventFileMoved     EventType = "file.moved"
	EventFileShared    EventType = "file.shared"

	// Terminal events
	EventTerminalStarted EventType = "terminal.started"
	EventTerminalOutput  EventType = "terminal.output"
	EventTerminalClosed  EventType = "terminal.closed"

	// Resource events
	EventResourceCreated EventType = "resource.created"
	EventResourceUpdated EventType = "resource.updated"
	EventResourceDeleted EventType = "resource.deleted"

	// User events
	EventUserJoined EventType = "user.joined"
	EventUserLeft   EventType = "user.left"

	// Notification events
	EventNotification EventType = "notification"

	// LabSession phase changes, published by dozlab-controller
	EventLabSessionPhaseChanged EventType = "labsession.phase_changed"
)

// Event represents a system event
type Event struct {
	ID         string                 `json:"id"`
	Type       EventType              `json:"type"`
	Source     string                 `json:"source"`    // Service that generated the event
	SessionID  string                 `json:"session_id,omitempty"`
	UserID     string                 `json:"user_id,omitempty"`
	LabID      string                 `json:"lab_id,omitempty"`
	Data       map[string]interface{} `json:"data"`
	Timestamp  time.Time              `json:"timestamp"`
	TTL        time.Duration          `json:"ttl,omitempty"` // Time to live
	IsCritical bool                   `json:"is_critical,omitempty"` // Whether event failure should halt operations
}

// EventHandler represents a function that handles events
type EventHandler func(ctx context.Context, event *Event) error

// EventPublisher publishes events to the event bus (messaging.RabbitEventBus).
type EventPublisher interface {
	Publish(ctx context.Context, event *Event) error
}

// ToEvent converts an *Event, an Event, or any other value (wrapped as a
// "generic.event" payload) to an *Event.
func ToEvent(event interface{}) *Event {
	switch e := event.(type) {
	case *Event:
		return e
	case Event:
		return &e
	default:
		return &Event{
			ID:        fmt.Sprintf("generic_%d", time.Now().UnixNano()),
			Type:      "generic.event",
			Source:    "unknown",
			Data:      map[string]interface{}{"payload": event},
			Timestamp: time.Now(),
		}
	}
}
