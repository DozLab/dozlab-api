package websocket

import (
	"context"
	"log"
	"time"
)

// HandleNotificationEvent is the event bus handler for EventNotification: it
// pushes the notification to every open connection of event.UserID. Delivery is
// best-effort: if the user has no connection the event is dropped, and it
// always returns nil so the bus acks instead of retrying (see docs/decision.md).
func (m *Manager) HandleNotificationEvent(ctx context.Context, event interface{}) error {
	e := ToEvent(event)
	if e.UserID == "" {
		log.Printf("notification %s has no user_id; dropped", e.ID)
		return nil
	}
	m.SendToUser(e.UserID, NotificationFromEvent(e))
	return nil
}

// NotificationFromEvent converts a notification event into the WebSocket message
// clients receive.
func NotificationFromEvent(e *Event) Message {
	notification := NotificationMessage{ID: e.ID}
	notification.Type, _ = e.Data["type"].(string)
	notification.Message, _ = e.Data["message"].(string)
	notification.Data = e.Data["data"]
	if sender, ok := e.Data["sender_id"].(string); ok {
		notification.SenderID = sender
	}

	timestamp := e.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return Message{
		Type:      MessageTypeNotification,
		SessionID: e.SessionID,
		Data:      notification,
		Timestamp: timestamp.Unix(),
	}
}
