package websocket

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// controllerEvent is a phase event as dozlab-controller publishes it
// (internal/events.PhaseChange.Event).
const controllerEvent = `{
	"id": "uid-1.Running",
	"type": "labsession.phase_changed",
	"source": "dozlab-controller",
	"session_id": "session-1",
	"user_id": "user-1",
	"data": {
		"phase": "Running",
		"message": "Lab session is running",
		"namespace": "dozlab-labs",
		"name": "lab-session-demo",
		"endpoints": {"terminal": "http://10.0.0.1:8081"}
	},
	"timestamp": "2026-09-27T22:00:00Z"
}`

func decodeControllerEvent(t *testing.T) *Event {
	t.Helper()
	var e Event
	if err := json.Unmarshal([]byte(controllerEvent), &e); err != nil {
		t.Fatalf("decode controller event: %v", err)
	}
	return &e
}

func TestSessionStatusFromPhaseEvent(t *testing.T) {
	e := decodeControllerEvent(t)
	if e.Type != EventLabSessionPhaseChanged {
		t.Fatalf("Type = %q, want %q", e.Type, EventLabSessionPhaseChanged)
	}

	msg := SessionStatusFromPhaseEvent(e)
	if msg.Type != MessageTypeSessionStatus || msg.SessionID != "session-1" ||
		msg.Timestamp != time.Date(2026, 9, 27, 22, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("unexpected envelope: %+v", msg)
	}
	want := map[string]interface{}{
		"status":     "running",
		"phase":      "Running",
		"session_id": "session-1",
		"user_id":    "user-1",
		"event_id":   "uid-1.Running",
		"message":    "Lab session is running",
		"endpoints":  map[string]interface{}{"terminal": "http://10.0.0.1:8081"},
	}
	if !reflect.DeepEqual(msg.Data, want) {
		t.Errorf("Data\n got %v\nwant %v", msg.Data, want)
	}
}

func TestSessionStatusFromPhaseEventKeepsReason(t *testing.T) {
	msg := SessionStatusFromPhaseEvent(&Event{
		ID: "uid-1.Failed", UserID: "u", SessionID: "s",
		Data: map[string]interface{}{"phase": "Failed", "message": "Pod failed", "reason": "pod is no longer ready"},
	})
	data := msg.Data.(map[string]interface{})
	if data["status"] != "failed" || data["reason"] != "pod is no longer ready" {
		t.Errorf("Data = %v", data)
	}
	if _, ok := data["endpoints"]; ok {
		t.Error("endpoints should be absent when the event has none")
	}
	if msg.Timestamp == 0 {
		t.Error("zero timestamp should fall back to now")
	}
}

func TestHandleLabSessionPhaseEventDeliversOnlyToOwner(t *testing.T) {
	m := NewManager()
	go m.Start()
	owner := &Client{ID: "c1", UserID: "user-1", Send: make(chan Message, 4), Manager: m}
	other := &Client{ID: "c2", UserID: "user-2", Send: make(chan Message, 4), Manager: m}
	m.register <- owner
	m.register <- other
	<-owner.Send // welcome
	<-other.Send

	if err := m.HandleLabSessionPhaseEvent(context.Background(), decodeControllerEvent(t)); err != nil {
		t.Fatalf("HandleLabSessionPhaseEvent: %v", err)
	}
	select {
	case msg := <-owner.Send:
		if msg.Type != MessageTypeSessionStatus || msg.Data.(map[string]interface{})["status"] != "running" {
			t.Errorf("unexpected message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("owner got nothing")
	}
	select {
	case msg := <-other.Send:
		t.Errorf("other user got %+v", msg)
	default:
	}
}

func TestHandleLabSessionPhaseEventAcksUndeliverable(t *testing.T) {
	m := NewManager()
	go m.Start()
	for _, e := range []*Event{
		{ID: "a", Type: EventLabSessionPhaseChanged, UserID: "offline", Data: map[string]interface{}{"phase": "Running"}},
		{ID: "b", Type: EventLabSessionPhaseChanged, Data: map[string]interface{}{"phase": "Running"}},
	} {
		if err := m.HandleLabSessionPhaseEvent(context.Background(), e); err != nil {
			t.Errorf("%s: got %v, want nil", e.ID, err)
		}
	}
}
