package services

import (
	"context"
	"fmt"
	"log"
	"time"
	"dozlab-backend/internal/websocket"
)

type eventBusService struct {
	redisEventBus *websocket.RedisEventBus
}

// NewEventBusService creates a new event bus service with proper error handling
func NewEventBusService(redisEventBus *websocket.RedisEventBus) EventBusService {
	return &eventBusService{
		redisEventBus: redisEventBus,
	}
}

// PublishEvent publishes an event with non-blocking error handling
func (s *eventBusService) PublishEvent(ctx context.Context, event interface{}) error {
	if err := s.redisEventBus.PublishEvent(ctx, event); err != nil {
		// Log error but don't halt operations for non-critical events
		log.Printf("Warning: Failed to publish non-critical event: %v", err)
		return nil // Return nil to continue operations - this is intentional for non-critical events
	}
	return nil
}

// PublishCriticalEvent publishes events that should halt operations on failure
func (s *eventBusService) PublishCriticalEvent(ctx context.Context, event interface{}) error {
	// Add retry logic for critical events
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if err := s.redisEventBus.PublishEvent(ctx, event); err != nil {
			log.Printf("Critical event publishing attempt %d failed: %v", i+1, err)
			if i == maxRetries-1 {
				// Return error for critical events to halt operations
				return fmt.Errorf("critical event publishing failed after %d attempts: %w", maxRetries, err)
			}
			// Wait briefly before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
		log.Printf("Critical event published successfully on attempt %d", i+1)
		return nil
	}
	return nil
}

// RegisterHandler registers an event handler
func (s *eventBusService) RegisterHandler(eventType string, handler func(ctx context.Context, event interface{}) error) {
	s.redisEventBus.RegisterHandler(websocket.EventType(eventType), func(ctx context.Context, event *websocket.Event) error {
		return handler(ctx, event)
	})
}

// Subscribe subscribes to event types
func (s *eventBusService) Subscribe(ctx context.Context, eventTypes []string) error {
	// Convert string event types to websocket.EventType
	wsEventTypes := make([]websocket.EventType, len(eventTypes))
	for i, et := range eventTypes {
		wsEventTypes[i] = websocket.EventType(et)
	}
	
	return s.redisEventBus.Subscribe(ctx, wsEventTypes...)
}

// GetEvent retrieves a stored event by ID
func (s *eventBusService) GetEvent(ctx context.Context, eventID string) (interface{}, error) {
	return s.redisEventBus.GetEvent(ctx, eventID)
}

// ListEvents lists events with filters
func (s *eventBusService) ListEvents(ctx context.Context, filters map[string]interface{}) ([]interface{}, error) {
	// Implementation would depend on your specific Redis storage pattern
	// This is a placeholder that would need to be implemented based on your event storage
	return nil, fmt.Errorf("list events not implemented yet")
}