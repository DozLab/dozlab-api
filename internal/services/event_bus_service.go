package services

import (
	"context"
	"fmt"
	"log"
	"time"
	"dozlab-backend/internal/websocket"
)

// EventTransport is the pub/sub transport behind EventBusService (messaging.RabbitEventBus).
type EventTransport interface {
	Publish(ctx context.Context, event *websocket.Event) error
	RegisterHandler(eventType websocket.EventType, handler websocket.EventHandler)
	Subscribe(ctx context.Context, eventTypes ...websocket.EventType) error
}

type eventBusService struct {
	transport EventTransport
}

// NewEventBusService creates a new event bus service with proper error handling
func NewEventBusService(transport EventTransport) EventBusService {
	return &eventBusService{transport: transport}
}

// publish converts event to a websocket.Event and publishes it
func (s *eventBusService) publish(ctx context.Context, event interface{}) error {
	return s.transport.Publish(ctx, websocket.ToEvent(event))
}

// PublishEvent publishes an event with non-blocking error handling
func (s *eventBusService) PublishEvent(ctx context.Context, event interface{}) error {
	if err := s.publish(ctx, event); err != nil {
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
		if err := s.publish(ctx, event); err != nil {
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
	s.transport.RegisterHandler(websocket.EventType(eventType), func(ctx context.Context, event *websocket.Event) error {
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
	
	return s.transport.Subscribe(ctx, wsEventTypes...)
}
