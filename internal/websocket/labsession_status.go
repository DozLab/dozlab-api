package websocket

import (
	"context"
	"log"
	"strings"
	"time"
)

// HandleLabSessionPhaseEvent is the event bus handler for
// EventLabSessionPhaseChanged (published by dozlab-controller): it pushes a
// session_status message to every open connection of event.UserID. Like
// notifications, delivery is best-effort and it always returns nil, so the bus
// acks instead of retrying. The controller may publish a phase twice with the
// same ID; clients can drop repeats by data.event_id.
func (m *Manager) HandleLabSessionPhaseEvent(ctx context.Context, event interface{}) error {
	e := ToEvent(event)
	if e.UserID == "" {
		log.Printf("labsession phase event %s has no user_id; dropped", e.ID)
		return nil
	}
	m.SendToUser(e.UserID, SessionStatusFromPhaseEvent(e))
	return nil
}

// SessionStatusFromPhaseEvent converts a LabSession phase event into the
// session_status message clients receive. "status" is the lowercased phase
// (pending, creating, running, failed, terminating), in line with the other
// session_status messages; "phase" keeps the controller's value.
func SessionStatusFromPhaseEvent(e *Event) Message {
	phase, _ := e.Data["phase"].(string)
	data := map[string]interface{}{
		"status":     strings.ToLower(phase),
		"phase":      phase,
		"session_id": e.SessionID,
		"user_id":    e.UserID,
		"event_id":   e.ID,
	}
	for _, key := range []string{"message", "reason", "endpoints"} {
		if v, ok := e.Data[key]; ok {
			data[key] = v
		}
	}

	timestamp := e.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return Message{
		Type:      MessageTypeSessionStatus,
		SessionID: e.SessionID,
		Data:      data,
		Timestamp: timestamp.Unix(),
	}
}
