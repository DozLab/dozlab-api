package messaging

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"dozlab-backend/internal/websocket"
)

func TestDefaultInstanceIDIsUniquePerProcess(t *testing.T) {
	a, b := defaultInstanceID(), defaultInstanceID()
	if a == b {
		t.Errorf("two IDs are equal: %s", a)
	}
	if !regexp.MustCompile(`^.+-[0-9a-f]{8}$`).MatchString(a) {
		t.Errorf("ID %q is not <hostname>-<8 hex>", a)
	}
}

func TestAddTypesRefusesTypesOnTheOtherQueue(t *testing.T) {
	group, instance := map[string]bool{}, map[string]bool{}
	if _, err := addTypes(group, instance, []websocket.EventType{"session.created"}); err != nil {
		t.Fatal(err)
	}
	if _, err := addTypes(instance, group, []websocket.EventType{"notification", "session.created"}); err == nil {
		t.Fatal("expected an error for a type already on the group queue")
	}
	if instance["notification"] {
		t.Error("a refused call must not record any of its types")
	}
	keys, err := addTypes(instance, group, []websocket.EventType{"notification"})
	if err != nil || len(keys) != 1 || !instance["notification"] {
		t.Errorf("addTypes = %v, %v; instance = %v", keys, err, instance)
	}
}

// Instance subscriptions use maxRetries 0: a failure goes straight to the DLQ,
// never to the group's retry queue (which would return it to the group queue).
func TestProcessWithoutRetriesDeadLettersFirstFailure(t *testing.T) {
	ack, pub, calls := &fakeAck{}, &fakePublisher{}, 0
	p := newProcessor(pub, errors.New("boom"), &calls)
	p.maxRetries = 0

	result, err := p.process(context.Background(), newDelivery(t, ack, nil, nil))
	if err != nil || result != outcomeDeadLettered {
		t.Fatalf("process = %v, %v; want dead-lettered", result, err)
	}
	if len(pub.sent) != 1 || pub.sent[0].key != DLQName || !ack.acked {
		t.Errorf("sent = %+v, acked = %v; want one message to %s and an ack", pub.sent, ack.acked, DLQName)
	}
}
