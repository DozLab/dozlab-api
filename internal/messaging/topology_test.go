package messaging

import (
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type declaredExchange struct {
	name, kind string
	durable    bool
}

type declaredQueue struct {
	name    string
	durable bool
	args    amqp.Table
}

type binding struct {
	queue, key, exchange string
}

// fakeDeclarer records topology declarations.
type fakeDeclarer struct {
	exchanges []declaredExchange
	queues    []declaredQueue
	bindings  []binding
}

func (f *fakeDeclarer) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	f.exchanges = append(f.exchanges, declaredExchange{name, kind, durable})
	return nil
}

func (f *fakeDeclarer) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	f.queues = append(f.queues, declaredQueue{name, durable, args})
	return amqp.Queue{Name: name}, nil
}

func (f *fakeDeclarer) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	f.bindings = append(f.bindings, binding{name, key, exchange})
	return nil
}

func TestDeclareBase(t *testing.T) {
	f := &fakeDeclarer{}
	if err := declareBase(f); err != nil {
		t.Fatal(err)
	}
	if want := []declaredExchange{{"dozlab.events", "topic", true}}; !reflect.DeepEqual(f.exchanges, want) {
		t.Errorf("exchanges = %+v, want %+v", f.exchanges, want)
	}
	if len(f.queues) != 1 || f.queues[0].name != "dozlab.events.dlq" || !f.queues[0].durable ||
		f.queues[0].args["x-queue-type"] != "quorum" {
		t.Errorf("queues = %+v, want durable quorum dozlab.events.dlq", f.queues)
	}
}

func TestDeclareGroup(t *testing.T) {
	f := &fakeDeclarer{}
	if err := declareGroup(f, "api", []string{"session.created", "lab.started"}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(f.queues) != 2 {
		t.Fatalf("declared %d queues, want 2", len(f.queues))
	}

	main := f.queues[0]
	if main.name != "dozlab.events.api" || !main.durable || !reflect.DeepEqual(main.args, amqp.Table{"x-queue-type": "quorum"}) {
		t.Errorf("main queue = %+v", main)
	}

	retry := f.queues[1]
	wantRetryArgs := amqp.Table{
		"x-queue-type":              "quorum",
		"x-message-ttl":             int64(10000),
		"x-dead-letter-exchange":    "dozlab.events",
		"x-dead-letter-routing-key": "retry.api",
	}
	if retry.name != "dozlab.events.api.retry" || !retry.durable || !reflect.DeepEqual(retry.args, wantRetryArgs) {
		t.Errorf("retry queue = %+v, want args %v", retry, wantRetryArgs)
	}

	wantBindings := []binding{
		{"dozlab.events.api", "retry.api", "dozlab.events"},
		{"dozlab.events.api", "session.created", "dozlab.events"},
		{"dozlab.events.api", "lab.started", "dozlab.events"},
	}
	if !reflect.DeepEqual(f.bindings, wantBindings) {
		t.Errorf("bindings = %+v, want %+v", f.bindings, wantBindings)
	}
}

func TestDeclareInstance(t *testing.T) {
	f := &fakeDeclarer{}
	queue := InstanceQueueName("api", "pod-1-abcd")
	if queue != "dozlab.events.api.instance.pod-1-abcd" {
		t.Fatalf("InstanceQueueName = %s", queue)
	}
	if err := declareInstance(f, queue, []string{"notification"}); err != nil {
		t.Fatal(err)
	}

	wantArgs := amqp.Table{"x-queue-type": "classic", "x-expires": int64(120000)}
	if len(f.queues) != 1 || f.queues[0].name != queue || f.queues[0].durable || !reflect.DeepEqual(f.queues[0].args, wantArgs) {
		t.Errorf("queues = %+v, want one non-durable %s with args %v", f.queues, queue, wantArgs)
	}
	if want := []binding{{queue, "notification", "dozlab.events"}}; !reflect.DeepEqual(f.bindings, want) {
		t.Errorf("bindings = %+v, want %+v", f.bindings, want)
	}
}
