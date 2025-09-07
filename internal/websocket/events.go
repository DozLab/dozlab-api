package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
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

// RedisEventBus implements a distributed event bus using Redis pub/sub
type RedisEventBus struct {
	client      *redis.Client
	handlers    map[EventType][]EventHandler
	subscribers map[string]*redis.PubSub
}

// NewRedisEventBus creates a new Redis-based event bus
func NewRedisEventBus(redisURL string) (*RedisEventBus, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisEventBus{
		client:      client,
		handlers:    make(map[EventType][]EventHandler),
		subscribers: make(map[string]*redis.PubSub),
	}, nil
}

// Publish publishes an event to the event bus
func (bus *RedisEventBus) Publish(ctx context.Context, event *Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	channel := bus.getChannelName(event.Type)
	
	// Publish to specific event type channel
	if err := bus.client.Publish(ctx, channel, eventData).Err(); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	// Also publish to global event channel for monitoring
	if err := bus.client.Publish(ctx, "events.all", eventData).Err(); err != nil {
		return fmt.Errorf("failed to publish to global channel: %w", err)
	}

	// Store event in Redis with TTL if specified
	if event.TTL > 0 {
		key := fmt.Sprintf("event:%s", event.ID)
		if err := bus.client.Set(ctx, key, eventData, event.TTL).Err(); err != nil {
			// Non-fatal error - log but don't fail publish
			fmt.Printf("Warning: failed to store event %s: %v\n", event.ID, err)
		}
	}

	return nil
}

// PublishEvent is an alias for Publish for backward compatibility
func (bus *RedisEventBus) PublishEvent(ctx context.Context, event interface{}) error {
	// Convert generic event to *Event
	switch e := event.(type) {
	case *Event:
		return bus.Publish(ctx, e)
	case Event:
		return bus.Publish(ctx, &e)
	default:
		// Create a generic event wrapper
		genericEvent := &Event{
			ID:        fmt.Sprintf("generic_%d", time.Now().UnixNano()),
			Type:      "generic.event",
			Source:    "unknown",
			Data:      map[string]interface{}{"payload": event},
			Timestamp: time.Now(),
		}
		return bus.Publish(ctx, genericEvent)
	}
}

// Subscribe subscribes to specific event types
func (bus *RedisEventBus) Subscribe(ctx context.Context, eventTypes ...EventType) error {
	channels := make([]string, len(eventTypes))
	for i, eventType := range eventTypes {
		channels[i] = bus.getChannelName(eventType)
	}

	pubsub := bus.client.Subscribe(ctx, channels...)
	
	// Store subscriber for cleanup
	subscriberID := fmt.Sprintf("sub_%d", time.Now().UnixNano())
	bus.subscribers[subscriberID] = pubsub

	go func() {
		defer pubsub.Close()
		
		ch := pubsub.Channel()
		for msg := range ch {
			var event Event
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				fmt.Printf("Error unmarshaling event: %v\n", err)
				continue
			}

			// Execute handlers for this event type
			handlers := bus.handlers[event.Type]
			for _, handler := range handlers {
				go func(h EventHandler) {
					if err := h(ctx, &event); err != nil {
						fmt.Printf("Error handling event %s: %v\n", event.ID, err)
						
						// For critical events, consider publishing failure notifications
						if event.IsCritical {
							failureEvent := Event{
								ID:        fmt.Sprintf("failure_%s", event.ID),
								Type:      "system.event_handler_failed",
								SessionID: event.SessionID,
								UserID:    event.UserID,
								Data: map[string]interface{}{
									"original_event_id": event.ID,
									"error":            err.Error(),
								},
								IsCritical: false, // Avoid infinite loops
								Timestamp:  time.Now(),
							}
							bus.Publish(context.Background(), &failureEvent)
						}
					}
				}(handler)
			}
		}
	}()

	return nil
}

// RegisterHandler registers an event handler for specific event types
func (bus *RedisEventBus) RegisterHandler(eventType EventType, handler EventHandler) {
	bus.handlers[eventType] = append(bus.handlers[eventType], handler)
}

// GetEvent retrieves a stored event by ID
func (bus *RedisEventBus) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	key := fmt.Sprintf("event:%s", eventID)
	
	result, err := bus.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("event not found")
		}
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	var event Event
	if err := json.Unmarshal([]byte(result), &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	return &event, nil
}

// ListEvents lists recent events of specific types
func (bus *RedisEventBus) ListEvents(ctx context.Context, eventTypes []EventType, limit int) ([]*Event, error) {
	var events []*Event

	// Search for events in Redis using pattern matching
	pattern := "event:*"
	keys, err := bus.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list event keys: %w", err)
	}

	// Limit the number of keys processed
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	for _, key := range keys {
		result, err := bus.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip errored keys
		}

		var event Event
		if err := json.Unmarshal([]byte(result), &event); err != nil {
			continue // Skip malformed events
		}

		// Filter by event types if specified
		if len(eventTypes) > 0 {
			found := false
			for _, eventType := range eventTypes {
				if event.Type == eventType {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		events = append(events, &event)
	}

	return events, nil
}

// CreateSessionEventStream creates a dedicated event stream for a session
func (bus *RedisEventBus) CreateSessionEventStream(ctx context.Context, sessionID string) error {
	streamKey := fmt.Sprintf("session:%s:events", sessionID)
	
	// Create stream by adding a dummy entry
	_, err := bus.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"type":      "session.stream_created",
			"timestamp": time.Now().Unix(),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to create session event stream: %w", err)
	}

	// Set TTL for the stream (24 hours)
	if err := bus.client.Expire(ctx, streamKey, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to set stream TTL: %w", err)
	}

	return nil
}

// AddSessionEvent adds an event to a session's event stream
func (bus *RedisEventBus) AddSessionEvent(ctx context.Context, sessionID string, event *Event) error {
	streamKey := fmt.Sprintf("session:%s:events", sessionID)
	
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = bus.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"event": string(eventData),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to add session event: %w", err)
	}

	return nil
}

// GetSessionEvents retrieves events from a session's event stream
func (bus *RedisEventBus) GetSessionEvents(ctx context.Context, sessionID string, start string, count int64) ([]*Event, error) {
	streamKey := fmt.Sprintf("session:%s:events", sessionID)
	
	if start == "" {
		start = "-" // Get from beginning
	}
	
	end := "+"
	if count > 0 {
		// Use XRANGE with COUNT
		args := &redis.XRangeArgs{
			Stream: streamKey,
			Start:  start,
			Stop:   end,
			Count:  count,
		}
		
		result, err := bus.client.XRange(ctx, args).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get session events: %w", err)
		}
		
		return bus.parseStreamMessages(result)
	}

	// Get all events in range
	result, err := bus.client.XRange(ctx, streamKey, start, end).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get session events: %w", err)
	}

	return bus.parseStreamMessages(result)
}

// CreateEventPattern creates a pattern-based event subscription
func (bus *RedisEventBus) CreateEventPattern(ctx context.Context, pattern string, handler EventHandler) error {
	pubsub := bus.client.PSubscribe(ctx, pattern)
	
	subscriberID := fmt.Sprintf("pattern_%d", time.Now().UnixNano())
	bus.subscribers[subscriberID] = pubsub

	go func() {
		defer pubsub.Close()
		
		ch := pubsub.Channel()
		for msg := range ch {
			var event Event
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				fmt.Printf("Error unmarshaling pattern event: %v\n", err)
				continue
			}

			go func() {
				if err := handler(ctx, &event); err != nil {
					fmt.Printf("Error handling pattern event %s: %v\n", event.ID, err)
				}
			}()
		}
	}()

	return nil
}

// Close closes all subscribers and the Redis connection
func (bus *RedisEventBus) Close() error {
	// Close all subscribers
	for _, pubsub := range bus.subscribers {
		if err := pubsub.Close(); err != nil {
			fmt.Printf("Error closing subscriber: %v\n", err)
		}
	}

	// Close Redis client
	return bus.client.Close()
}

// Helper methods

// getChannelName generates Redis channel name for event type
func (bus *RedisEventBus) getChannelName(eventType EventType) string {
	return fmt.Sprintf("events.%s", string(eventType))
}

// parseStreamMessages converts Redis stream messages to events
func (bus *RedisEventBus) parseStreamMessages(messages []redis.XMessage) ([]*Event, error) {
	events := make([]*Event, 0, len(messages))
	
	for _, msg := range messages {
		eventData, exists := msg.Values["event"]
		if !exists {
			continue
		}

		eventJSON, ok := eventData.(string)
		if !ok {
			continue
		}

		var event Event
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			continue // Skip malformed events
		}

		events = append(events, &event)
	}

	return events, nil
}

// EventMetrics provides metrics about the event system
type EventMetrics struct {
	TotalEvents     int64            `json:"total_events"`
	EventsByType    map[string]int64 `json:"events_by_type"`
	ActiveSessions  int64            `json:"active_sessions"`
	EventRate       float64          `json:"event_rate"` // Events per second
	LastEventTime   time.Time        `json:"last_event_time"`
}

// GetMetrics returns metrics about the event system
func (bus *RedisEventBus) GetMetrics(ctx context.Context) (*EventMetrics, error) {
	metrics := &EventMetrics{
		EventsByType: make(map[string]int64),
	}

	// Count total events stored
	pattern := "event:*"
	keys, err := bus.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to count events: %w", err)
	}
	metrics.TotalEvents = int64(len(keys))

	// Count active session streams
	pattern = "session:*:events"
	sessionKeys, err := bus.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to count session streams: %w", err)
	}
	metrics.ActiveSessions = int64(len(sessionKeys))

	// Sample recent events for type breakdown
	if len(keys) > 0 {
		sampleSize := 100
		if len(keys) < sampleSize {
			sampleSize = len(keys)
		}

		for i := 0; i < sampleSize; i++ {
			result, err := bus.client.Get(ctx, keys[i]).Result()
			if err != nil {
				continue
			}

			var event Event
			if err := json.Unmarshal([]byte(result), &event); err != nil {
				continue
			}

			eventType := string(event.Type)
			metrics.EventsByType[eventType]++
			
			if metrics.LastEventTime.IsZero() || event.Timestamp.After(metrics.LastEventTime) {
				metrics.LastEventTime = event.Timestamp
			}
		}

		// Calculate rough event rate (events per second over last hour)
		if !metrics.LastEventTime.IsZero() {
			duration := time.Since(metrics.LastEventTime)
			if duration.Seconds() > 0 {
				metrics.EventRate = float64(metrics.TotalEvents) / duration.Seconds()
			}
		}
	}

	return metrics, nil
}