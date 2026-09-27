// Package messaging implements the event bus on RabbitMQ.
//
// Topology (declared idempotently on connect and on subscribe):
//
//	dozlab.events               durable topic exchange; routing key = event type
//	dozlab.events.<group>       durable quorum queue per consumer group, bound to the
//	                            event types the group handles and to retry.<group>
//	dozlab.events.<group>.retry durable quorum queue; per-queue TTL (retry delay), then
//	                            dead-letters to dozlab.events with key retry.<group>, so a
//	                            retried message returns only to the group that failed it
//	dozlab.events.dlq           durable quorum queue for messages that failed too often
//	dozlab.events.<group>.instance.<id>
//	                            per-process classic queue (not durable, deleted by the
//	                            broker after instanceQueueExpiry without a consumer),
//	                            bound to event types every instance must receive
//
// Consumers ack only after every handler succeeds. On failure the message is
// republished to the group's retry queue with an incremented retry-count header;
// once the count reaches the configured maximum it goes to the DLQ instead.
// Instance queues have no retry queue: a failed message goes straight to the DLQ.
package messaging

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// ExchangeName is the topic exchange all events are published to.
	ExchangeName = "dozlab.events"
	// DLQName is the queue for messages that exhausted their retries.
	DLQName = "dozlab.events.dlq"

	// Headers set on retried and dead-lettered messages.
	HeaderRetryCount          = "x-dozlab-retry-count"
	HeaderLastError           = "x-dozlab-last-error"
	HeaderConsumerGroup       = "x-dozlab-consumer-group"
	HeaderOriginalRoutingKey  = "x-dozlab-original-routing-key"
	HeaderDeadLetteredReason  = "x-dozlab-dead-letter-reason"
	deadLetterReasonMalformed = "malformed"
	deadLetterReasonRetries   = "max-retries"
)

// QueueName is the main queue of a consumer group.
func QueueName(group string) string { return "dozlab.events." + group }

// RetryQueueName is the retry queue of a consumer group.
func RetryQueueName(group string) string { return QueueName(group) + ".retry" }

// InstanceQueueName is the per-process queue of one instance of a consumer group.
func InstanceQueueName(group, instanceID string) string {
	return QueueName(group) + ".instance." + instanceID
}

// instanceQueueExpiry is how long an instance queue outlives its consumer, so it
// survives a reconnect but is removed after its process is gone.
const instanceQueueExpiry = 2 * time.Minute

// retryRoutingKey routes a message from a group's retry queue back to that group only.
func retryRoutingKey(group string) string { return "retry." + group }

// declarer is the part of an AMQP channel that declares topology.
type declarer interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
}

func quorumArgs() amqp.Table {
	return amqp.Table{"x-queue-type": "quorum"}
}

// declareBase declares the exchange and the shared DLQ.
func declareBase(ch declarer) error {
	if err := ch.ExchangeDeclare(ExchangeName, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", ExchangeName, err)
	}
	if _, err := ch.QueueDeclare(DLQName, true, false, false, false, quorumArgs()); err != nil {
		return fmt.Errorf("declare queue %s: %w", DLQName, err)
	}
	return nil
}

// declareGroup declares a consumer group's main and retry queues and binds the
// main queue to eventTypes. Changing retryDelay for an existing group fails with
// PRECONDITION_FAILED until the retry queue is deleted.
func declareGroup(ch declarer, group string, eventTypes []string, retryDelay time.Duration) error {
	queue, retryQueue := QueueName(group), RetryQueueName(group)

	if _, err := ch.QueueDeclare(queue, true, false, false, false, quorumArgs()); err != nil {
		return fmt.Errorf("declare queue %s: %w", queue, err)
	}

	retryArgs := quorumArgs()
	retryArgs["x-message-ttl"] = retryDelay.Milliseconds()
	retryArgs["x-dead-letter-exchange"] = ExchangeName
	retryArgs["x-dead-letter-routing-key"] = retryRoutingKey(group)
	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, retryArgs); err != nil {
		return fmt.Errorf("declare queue %s: %w", retryQueue, err)
	}

	keys := append([]string{retryRoutingKey(group)}, eventTypes...)
	for _, key := range keys {
		if err := ch.QueueBind(queue, key, ExchangeName, false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", queue, key, err)
		}
	}
	return nil
}

// declareInstance declares an instance queue and binds it to eventTypes. It is
// a classic queue, not durable: it holds events for connections that live only
// as long as the process.
func declareInstance(ch declarer, queue string, eventTypes []string) error {
	args := amqp.Table{
		"x-queue-type": "classic",
		"x-expires":    instanceQueueExpiry.Milliseconds(),
	}
	if _, err := ch.QueueDeclare(queue, false, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue %s: %w", queue, err)
	}
	for _, key := range eventTypes {
		if err := ch.QueueBind(queue, key, ExchangeName, false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", queue, key, err)
		}
	}
	return nil
}
