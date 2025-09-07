package events

import (
	"context"
	"encoding/json"
	"time"

	"dozlab-backend/internal/clients"
	"github.com/google/uuid"
)

// EventType represents different types of events
type EventType string

const (
	// Lab session events
	EventSessionCreated   EventType = "session.created"
	EventSessionStarted   EventType = "session.started"
	EventSessionCompleted EventType = "session.completed"
	EventSessionFailed    EventType = "session.failed"
	EventSessionTerminated EventType = "session.terminated"
	
	// User events
	EventUserRegistered EventType = "user.registered"
	EventUserLoggedIn   EventType = "user.logged_in"
	
	// Lab events
	EventLabCreated    EventType = "lab.created"
	EventLabPublished  EventType = "lab.published"
	EventLabValidated  EventType = "lab.validated"
	
	// Notification events
	EventNotificationSent EventType = "notification.sent"
	EventEmailSent        EventType = "email.sent"
	
	// Workflow events
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
)

// Event represents a domain event in the system
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Source    string                 `json:"source"`
	Subject   string                 `json:"subject"` // e.g., user ID, session ID
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Version   string                 `json:"version"`
}

// EventBus handles event publishing and subscription
type EventBus struct {
	redis *clients.RedisClient
}

// NewEventBus creates a new event bus
func NewEventBus(redis *clients.RedisClient) *EventBus {
	return &EventBus{
		redis: redis,
	}
}

// PublishEvent publishes an event to the appropriate channel
func (eb *EventBus) PublishEvent(ctx context.Context, eventType EventType, source, subject string, data map[string]interface{}) error {
	event := Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		Source:    source,
		Subject:   subject,
		Data:      data,
		Timestamp: time.Now(),
		Version:   "1.0",
	}
	
	// Publish to specific event channel
	channel := string(eventType)
	if err := eb.redis.Publish(ctx, channel, event); err != nil {
		return err
	}
	
	// Also publish to general events channel for event sourcing
	return eb.redis.Publish(ctx, "events.all", event)
}

// Session-related convenience methods

// PublishSessionCreated publishes a session created event
func (eb *EventBus) PublishSessionCreated(ctx context.Context, sessionID, userID string, sessionData map[string]interface{}) error {
	return eb.PublishEvent(ctx, EventSessionCreated, "api-service", sessionID, map[string]interface{}{
		"session_id": sessionID,
		"user_id":    userID,
		"data":       sessionData,
	})
}

// PublishSessionStarted publishes a session started event
func (eb *EventBus) PublishSessionStarted(ctx context.Context, sessionID string, endpoints map[string]string) error {
	return eb.PublishEvent(ctx, EventSessionStarted, "controller", sessionID, map[string]interface{}{
		"session_id": sessionID,
		"endpoints":  endpoints,
	})
}

// PublishSessionCompleted publishes a session completed event
func (eb *EventBus) PublishSessionCompleted(ctx context.Context, sessionID string, results map[string]interface{}) error {
	return eb.PublishEvent(ctx, EventSessionCompleted, "examiner-service", sessionID, map[string]interface{}{
		"session_id": sessionID,
		"results":    results,
	})
}

// User-related convenience methods

// PublishUserRegistered publishes a user registered event
func (eb *EventBus) PublishUserRegistered(ctx context.Context, userID string, userData map[string]interface{}) error {
	return eb.PublishEvent(ctx, EventUserRegistered, "api-service", userID, map[string]interface{}{
		"user_id": userID,
		"data":    userData,
	})
}

// Lab-related convenience methods

// PublishLabCreated publishes a lab created event
func (eb *EventBus) PublishLabCreated(ctx context.Context, labID, creatorID string, labData map[string]interface{}) error {
	return eb.PublishEvent(ctx, EventLabCreated, "api-service", labID, map[string]interface{}{
		"lab_id":     labID,
		"creator_id": creatorID,
		"data":       labData,
	})
}

// PublishNotificationSent publishes a notification sent event
func (eb *EventBus) PublishNotificationSent(ctx context.Context, userID, notificationType string, message interface{}) error {
	return eb.PublishEvent(ctx, EventNotificationSent, "websocket-service", userID, map[string]interface{}{
		"user_id": userID,
		"type":    notificationType,
		"message": message,
	})
}

// EventHandler represents a function that handles events
type EventHandler func(ctx context.Context, event Event) error

// EventSubscriber handles event subscriptions
type EventSubscriber struct {
	redis    *clients.RedisClient
	handlers map[EventType][]EventHandler
}

// NewEventSubscriber creates a new event subscriber
func NewEventSubscriber(redis *clients.RedisClient) *EventSubscriber {
	return &EventSubscriber{
		redis:    redis,
		handlers: make(map[EventType][]EventHandler),
	}
}

// Subscribe registers a handler for a specific event type
func (es *EventSubscriber) Subscribe(eventType EventType, handler EventHandler) {
	es.handlers[eventType] = append(es.handlers[eventType], handler)
}

// Start starts listening for events
func (es *EventSubscriber) Start(ctx context.Context) error {
	// Create channels list from registered handlers
	var channels []string
	for eventType := range es.handlers {
		channels = append(channels, string(eventType))
	}
	
	if len(channels) == 0 {
		return nil // No handlers registered
	}
	
	// Subscribe to Redis channels
	pubsub := es.redis.Subscribe(ctx, channels...)
	defer pubsub.Close()
	
	// Listen for messages
	ch := pubsub.Channel()
	for {
		select {
		case msg := <-ch:
			var event Event
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue // Skip malformed events
			}
			
			// Call handlers for this event type
			if handlers, exists := es.handlers[event.Type]; exists {
				for _, handler := range handlers {
					go func(h EventHandler, e Event) {
						if err := h(ctx, e); err != nil {
							// Log error (in production, you might want to use a proper logger)
							// log.Printf("Event handler error: %v", err)
						}
					}(handler, event)
				}
			}
			
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Common event data structures

// SessionEventData represents data for session events
type SessionEventData struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	LabID     string                 `json:"lab_id"`
	Status    string                 `json:"status"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// UserEventData represents data for user events
type UserEventData struct {
	UserID string                 `json:"user_id"`
	Email  string                 `json:"email"`
	Role   string                 `json:"role"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// LabEventData represents data for lab events
type LabEventData struct {
	LabID     string                 `json:"lab_id"`
	CreatorID string                 `json:"creator_id"`
	Name      string                 `json:"name"`
	Status    string                 `json:"status"`
	Data      map[string]interface{} `json:"data,omitempty"`
}