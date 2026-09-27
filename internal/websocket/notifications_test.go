package websocket

import (
	"context"
	"testing"
	"time"
)

func TestNotificationFromEvent(t *testing.T) {
	ts := time.Date(2026, 9, 27, 21, 0, 0, 0, time.UTC)
	msg := NotificationFromEvent(&Event{
		ID:        "n-1",
		Type:      EventNotification,
		UserID:    "u-1",
		SessionID: "s-1",
		Data: map[string]interface{}{
			"type":      "lab_ready",
			"message":   "Your lab is ready",
			"data":      map[string]interface{}{"lab_id": "l-1"},
			"sender_id": "sender-1",
		},
		Timestamp: ts,
	})

	if msg.Type != MessageTypeNotification || msg.SessionID != "s-1" || msg.Timestamp != ts.Unix() {
		t.Fatalf("unexpected message envelope: %+v", msg)
	}
	n, ok := msg.Data.(NotificationMessage)
	if !ok {
		t.Fatalf("Data is %T, want NotificationMessage", msg.Data)
	}
	if n.ID != "n-1" || n.Type != "lab_ready" || n.Message != "Your lab is ready" || n.SenderID != "sender-1" {
		t.Errorf("unexpected notification: %+v", n)
	}
	if d, _ := n.Data.(map[string]interface{}); d["lab_id"] != "l-1" {
		t.Errorf("Data.data = %v", n.Data)
	}
}

func TestHandleNotificationEventDeliversOnlyToTargetUser(t *testing.T) {
	m := NewManager()
	go m.Start()
	target := &Client{ID: "c1", UserID: "u-1", Send: make(chan Message, 4), Manager: m}
	other := &Client{ID: "c2", UserID: "u-2", Send: make(chan Message, 4), Manager: m}
	m.register <- target
	m.register <- other
	<-target.Send // welcome
	<-other.Send

	err := m.HandleNotificationEvent(context.Background(), &Event{
		ID: "n-1", Type: EventNotification, UserID: "u-1",
		Data: map[string]interface{}{"type": "info", "message": "hi"},
	})
	if err != nil {
		t.Fatalf("HandleNotificationEvent: %v", err)
	}

	select {
	case msg := <-target.Send:
		if n := msg.Data.(NotificationMessage); n.ID != "n-1" || n.Message != "hi" {
			t.Errorf("unexpected notification: %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("target user got nothing")
	}
	select {
	case msg := <-other.Send:
		t.Errorf("other user got %+v", msg)
	default:
	}
}

// Offline users and events without a user are acked (nil), not retried.
func TestHandleNotificationEventDropsUndeliverable(t *testing.T) {
	m := NewManager()
	go m.Start()
	for _, e := range []*Event{
		{ID: "n-1", Type: EventNotification, UserID: "offline"},
		{ID: "n-2", Type: EventNotification},
	} {
		if err := m.HandleNotificationEvent(context.Background(), e); err != nil {
			t.Errorf("%s: got %v, want nil", e.ID, err)
		}
	}
}
