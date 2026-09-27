package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"dozlab-backend/internal/websocket"

	amqp "github.com/rabbitmq/amqp091-go"
)

// These tests need a RabbitMQ broker, e.g.
//
//	docker run -d --name dozlab-rabbitmq -p 5672:5672 rabbitmq:3-management
//	RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test ./internal/messaging/
//
// Each test uses its own consumer group and event type and deletes its queues.
func rabbitURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL not set; skipping RabbitMQ integration test")
	}
	return url
}

func newTestBus(t *testing.T, group string, maxRetries int) *RabbitEventBus {
	t.Helper()
	bus, err := NewRabbitEventBus(Config{
		URL:           rabbitURL(t),
		ConsumerGroup: group,
		Prefetch:      4,
		MaxRetries:    maxRetries,
		RetryDelay:    200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRabbitEventBus: %v", err)
	}
	t.Cleanup(func() {
		bus.Close()
		deleteQueues(t, QueueName(group), RetryQueueName(group))
	})
	return bus
}

func deleteQueues(t *testing.T, names ...string) {
	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		t.Logf("cleanup: %v", err)
		return
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Logf("cleanup: %v", err)
		return
	}
	defer ch.Close()
	for _, n := range names {
		if _, err := ch.QueueDelete(n, false, false, false); err != nil {
			t.Logf("cleanup %s: %v", n, err)
		}
	}
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestRabbitPublishSubscribeRetry(t *testing.T) {
	group := uniqueName("it-retry")
	eventType := websocket.EventType(uniqueName("test.retry"))
	bus := newTestBus(t, group, 3)

	var calls atomic.Int32
	bus.RegisterHandler(eventType, func(ctx context.Context, e *websocket.Event) error {
		if calls.Add(1) < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	if err := bus.Subscribe(context.Background(), eventType); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Subscribe returns after the bindings exist, so publish right away.

	if err := bus.Publish(context.Background(), &websocket.Event{ID: "it-1", Type: eventType}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Two failures go through the retry queue (200ms TTL each), then success.
	waitFor(t, 10*time.Second, func() bool { return calls.Load() == 3 })
	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got != 3 {
		t.Errorf("handler ran %d times, want 3 (acked after success)", got)
	}
}

func TestRabbitDeadLettersAfterMaxRetries(t *testing.T) {
	group := uniqueName("it-dlq")
	eventType := websocket.EventType(uniqueName("test.dlq"))
	bus := newTestBus(t, group, 1)

	var calls atomic.Int32
	bus.RegisterHandler(eventType, func(ctx context.Context, e *websocket.Event) error {
		calls.Add(1)
		return errors.New("always fails")
	})
	if err := bus.Subscribe(context.Background(), eventType); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	id := uniqueName("it-dlq-msg")
	if err := bus.Publish(context.Background(), &websocket.Event{ID: id, Type: eventType}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// First attempt + 1 retry, then the DLQ.
	var dead *amqp.Delivery
	waitFor(t, 10*time.Second, func() bool {
		dead = takeFromDLQ(t, id)
		return dead != nil
	})
	if got := calls.Load(); got != 2 {
		t.Errorf("handler ran %d times, want 2", got)
	}
	if dead.Headers[HeaderConsumerGroup] != group || dead.Headers[HeaderDeadLetteredReason] != deadLetterReasonRetries {
		t.Errorf("DLQ headers = %v", dead.Headers)
	}
	var e websocket.Event
	if err := json.Unmarshal(dead.Body, &e); err != nil || e.ID != id {
		t.Errorf("DLQ body = %s (err %v), want event %s", dead.Body, err, id)
	}
}

// takeFromDLQ removes and returns the DLQ message with the given ID, if present.
// Other messages are held unacked and requeued when the channel closes.
func takeFromDLQ(t *testing.T, id string) *amqp.Delivery {
	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		return nil
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return nil
	}
	defer ch.Close()
	for {
		d, ok, err := ch.Get(DLQName, false)
		if err != nil || !ok {
			return nil
		}
		if d.MessageId == id {
			d.Ack(false)
			return &d
		}
	}
}

func TestRabbitReconnects(t *testing.T) {
	group := uniqueName("it-reconnect")
	eventType := websocket.EventType(uniqueName("test.reconnect"))
	bus := newTestBus(t, group, 3)

	var calls atomic.Int32
	bus.RegisterHandler(eventType, func(ctx context.Context, e *websocket.Event) error {
		calls.Add(1)
		return nil
	})
	if err := bus.Subscribe(context.Background(), eventType); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Drop the connection underneath the bus.
	bus.connMu.Lock()
	old := bus.conn
	bus.connMu.Unlock()
	if err := old.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}

	// Publishing fails until the supervisor reconnects, then succeeds.
	waitFor(t, 10*time.Second, func() bool {
		return bus.Publish(context.Background(), &websocket.Event{ID: "it-reconnect", Type: eventType}) == nil
	})
	bus.connMu.Lock()
	reconnected := bus.conn != old
	bus.connMu.Unlock()
	if !reconnected {
		t.Error("expected a new connection after the drop")
	}
	// The consumer restarts on the new connection and receives the event.
	waitFor(t, 10*time.Second, func() bool { return calls.Load() >= 1 })
}
