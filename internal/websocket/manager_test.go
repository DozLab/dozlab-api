package websocket

import (
	"testing"
	"time"
)

// registerSlowClient registers a client whose Send buffer is already full
// after the welcome message.
func registerSlowClient(t *testing.T, m *Manager, id, userID string) {
	t.Helper()
	m.register <- &Client{ID: id, UserID: userID, Send: make(chan Message, 1), Manager: m}
}

func waitForClients(t *testing.T, m *Manager, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.GetStats()["total_clients"] == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("total_clients = %v, want %d", m.GetStats()["total_clients"], want)
}

// Before unregisterLater, a second slow client made SendToUser block on
// m.unregister while holding the read lock that unregisterClient waits for.
func TestSendToUserUnregistersSlowClientsWithoutDeadlock(t *testing.T) {
	m := NewManager()
	go m.Start()
	registerSlowClient(t, m, "c1", "u1")
	registerSlowClient(t, m, "c2", "u1")
	waitForClients(t, m, 2)

	done := make(chan struct{})
	go func() {
		m.SendToUser("u1", Message{Type: MessageTypeNotification})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SendToUser deadlocked")
	}
	waitForClients(t, m, 0)
}

// Before unregisterLater, broadcastMessage (on the Start goroutine) sent to
// m.unregister, which only that goroutine reads.
func TestBroadcastUnregistersSlowClientsWithoutDeadlock(t *testing.T) {
	m := NewManager()
	go m.Start()
	registerSlowClient(t, m, "c1", "u1")
	registerSlowClient(t, m, "c2", "u2")
	waitForClients(t, m, 2)

	m.Broadcast(Message{Type: MessageTypeNotification})
	waitForClients(t, m, 0)
}
