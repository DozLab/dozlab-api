package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"dozlab-backend/internal/websocket"

	amqp "github.com/rabbitmq/amqp091-go"
)

// fakeAck records what the processor did with a delivery.
type fakeAck struct {
	acked, nacked, requeued bool
}

func (f *fakeAck) Ack(tag uint64, multiple bool) error { f.acked = true; return nil }
func (f *fakeAck) Nack(tag uint64, multiple, requeue bool) error {
	f.nacked, f.requeued = true, requeue
	return nil
}
func (f *fakeAck) Reject(tag uint64, requeue bool) error { return errors.New("unexpected reject") }

type published struct {
	exchange, key string
	msg           amqp.Publishing
}

// fakePublisher records republished messages and can fail.
type fakePublisher struct {
	sent []published
	err  error
}

func (f *fakePublisher) publish(ctx context.Context, exchange, key string, msg amqp.Publishing) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, published{exchange, key, msg})
	return nil
}

func newDelivery(t *testing.T, ack amqp.Acknowledger, headers amqp.Table, body []byte) amqp.Delivery {
	t.Helper()
	if body == nil {
		var err error
		body, err = json.Marshal(websocket.Event{ID: "e1", Type: websocket.EventSessionCreated})
		if err != nil {
			t.Fatal(err)
		}
	}
	return amqp.Delivery{
		Acknowledger: ack,
		Headers:      headers,
		RoutingKey:   string(websocket.EventSessionCreated),
		MessageId:    "e1",
		ContentType:  "application/json",
		Body:         body,
	}
}

func newProcessor(pub republisher, handlerErr error, calls *int) *processor {
	return &processor{
		group:      "api",
		maxRetries: 3,
		dispatch: func(ctx context.Context, e *websocket.Event) error {
			*calls++
			return handlerErr
		},
		pub:     pub,
		moveCtx: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	}
}

func TestProcessAcksOnSuccess(t *testing.T) {
	ack, pub, calls := &fakeAck{}, &fakePublisher{}, 0
	result, err := newProcessor(pub, nil, &calls).process(context.Background(), newDelivery(t, ack, nil, nil))

	if err != nil || result != outcomeAcked {
		t.Fatalf("result=%d err=%v, want acked", result, err)
	}
	if !ack.acked || ack.nacked || calls != 1 || len(pub.sent) != 0 {
		t.Errorf("acked=%v nacked=%v calls=%d republished=%d", ack.acked, ack.nacked, calls, len(pub.sent))
	}
}

func TestProcessRetriesOnFailure(t *testing.T) {
	tests := []struct {
		name      string
		headers   amqp.Table
		wantCount int64
	}{
		{"first failure", nil, 1},
		{"second failure", amqp.Table{HeaderRetryCount: int32(1)}, 2},
		{"last retry", amqp.Table{HeaderRetryCount: int64(2)}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ack, pub, calls := &fakeAck{}, &fakePublisher{}, 0
			result, err := newProcessor(pub, errors.New("boom"), &calls).
				process(context.Background(), newDelivery(t, ack, tt.headers, nil))

			if err != nil || result != outcomeRetried {
				t.Fatalf("result=%d err=%v, want retried", result, err)
			}
			if !ack.acked || ack.nacked {
				t.Errorf("original must be acked after the retry copy is confirmed: acked=%v nacked=%v", ack.acked, ack.nacked)
			}
			if len(pub.sent) != 1 {
				t.Fatalf("republished %d messages, want 1", len(pub.sent))
			}
			got := pub.sent[0]
			if got.exchange != "" || got.key != "dozlab.events.api.retry" {
				t.Errorf("sent to %q/%q, want default exchange / dozlab.events.api.retry", got.exchange, got.key)
			}
			if got.msg.Headers[HeaderRetryCount] != tt.wantCount {
				t.Errorf("retry count = %v, want %d", got.msg.Headers[HeaderRetryCount], tt.wantCount)
			}
			if got.msg.Headers[HeaderLastError] != "boom" || got.msg.Headers[HeaderOriginalRoutingKey] != "session.created" {
				t.Errorf("headers = %v", got.msg.Headers)
			}
			if got.msg.DeliveryMode != amqp.Persistent {
				t.Errorf("delivery mode = %d, want persistent", got.msg.DeliveryMode)
			}
		})
	}
}

func TestProcessDeadLettersAfterMaxRetries(t *testing.T) {
	ack, pub, calls := &fakeAck{}, &fakePublisher{}, 0
	headers := amqp.Table{HeaderRetryCount: int64(3), HeaderOriginalRoutingKey: "session.created"}
	d := newDelivery(t, ack, headers, nil)
	d.RoutingKey = "retry.api" // redelivered from the retry queue
	result, err := newProcessor(pub, errors.New("still broken"), &calls).process(context.Background(), d)

	if err != nil || result != outcomeDeadLettered {
		t.Fatalf("result=%d err=%v, want dead-lettered", result, err)
	}
	if !ack.acked || len(pub.sent) != 1 || pub.sent[0].key != DLQName {
		t.Fatalf("acked=%v sent=%+v, want one message to %s", ack.acked, pub.sent, DLQName)
	}
	h := pub.sent[0].msg.Headers
	if h[HeaderDeadLetteredReason] != deadLetterReasonRetries || h[HeaderLastError] != "still broken" ||
		h[HeaderConsumerGroup] != "api" || h[HeaderOriginalRoutingKey] != "session.created" {
		t.Errorf("DLQ headers = %v", h)
	}
}

func TestProcessDeadLettersMalformedBody(t *testing.T) {
	ack, pub, calls := &fakeAck{}, &fakePublisher{}, 0
	result, err := newProcessor(pub, nil, &calls).
		process(context.Background(), newDelivery(t, ack, nil, []byte("{not json")))

	if err != nil || result != outcomeDeadLettered {
		t.Fatalf("result=%d err=%v, want dead-lettered", result, err)
	}
	if calls != 0 {
		t.Errorf("handler ran %d times for a malformed body", calls)
	}
	if len(pub.sent) != 1 || pub.sent[0].msg.Headers[HeaderDeadLetteredReason] != deadLetterReasonMalformed {
		t.Errorf("sent = %+v, want one malformed message in the DLQ", pub.sent)
	}
}

func TestProcessRequeuesWhenRepublishFails(t *testing.T) {
	ack, calls := &fakeAck{}, 0
	pub := &fakePublisher{err: errors.New("connection lost")}
	result, err := newProcessor(pub, errors.New("boom"), &calls).
		process(context.Background(), newDelivery(t, ack, nil, nil))

	if err == nil || result != outcomeRequeued {
		t.Fatalf("result=%d err=%v, want requeued with an error", result, err)
	}
	if ack.acked || !ack.nacked || !ack.requeued {
		t.Errorf("acked=%v nacked=%v requeued=%v, want nack with requeue", ack.acked, ack.nacked, ack.requeued)
	}
}

func TestRetryCount(t *testing.T) {
	tests := []struct {
		name string
		v    interface{}
		want int
	}{
		{"missing", nil, 0},
		{"int32", int32(2), 2},
		{"int64", int64(4), 4},
		{"string", "3", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := amqp.Table{}
			if tt.v != nil {
				h[HeaderRetryCount] = tt.v
			}
			if got := retryCount(h); got != tt.want {
				t.Errorf("retryCount = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	good := Config{URL: "amqp://x", ConsumerGroup: "api", Prefetch: 1, MaxRetries: 0, RetryDelay: 1}
	if err := good.validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	bad := Config{}
	if err := bad.validate(); err == nil {
		t.Error("empty config should fail validation")
	}
}
