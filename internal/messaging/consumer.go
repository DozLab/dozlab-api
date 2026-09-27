package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"dozlab-backend/internal/websocket"

	amqp "github.com/rabbitmq/amqp091-go"
)

// maxErrorHeaderLen bounds the error text stored on retried/dead-lettered messages.
const maxErrorHeaderLen = 1024

// outcome is what happened to a delivery.
type outcome int

const (
	outcomeAcked        outcome = iota // handled successfully (or nothing to do)
	outcomeRetried                     // moved to the retry queue
	outcomeDeadLettered                // moved to the DLQ
	outcomeRequeued                    // could not be moved; nacked back onto the queue
)

// republisher publishes a message and waits for the broker's confirm.
type republisher interface {
	publish(ctx context.Context, exchange, key string, msg amqp.Publishing) error
}

// processor handles deliveries for one consumer group.
type processor struct {
	group      string
	maxRetries int
	dispatch   func(ctx context.Context, event *websocket.Event) error
	pub        republisher
	// moveCtx gives republishing its own deadline, so failed messages are still
	// moved while the bus shuts down.
	moveCtx func() (context.Context, context.CancelFunc)
}

// process handles one delivery and acks, moves or requeues it. The delivery is
// only acked after the handlers succeed or the message is safely republished.
func (p *processor) process(ctx context.Context, d amqp.Delivery) (outcome, error) {
	var event websocket.Event
	if err := json.Unmarshal(d.Body, &event); err != nil {
		return p.move(d, DLQName, deadLetterReasonMalformed, err)
	}

	handlerErr := p.dispatch(ctx, &event)
	if handlerErr == nil {
		return outcomeAcked, d.Ack(false)
	}

	if retryCount(d.Headers) >= p.maxRetries {
		return p.move(d, DLQName, deadLetterReasonRetries, handlerErr)
	}
	return p.move(d, RetryQueueName(p.group), "", handlerErr)
}

// move republishes d to queue (through the default exchange) and acks it. If
// the republish fails, d is nacked and requeued so it is not lost.
func (p *processor) move(d amqp.Delivery, queue, deadLetterReason string, cause error) (outcome, error) {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	if _, ok := headers[HeaderOriginalRoutingKey]; !ok {
		headers[HeaderOriginalRoutingKey] = d.RoutingKey
	}
	headers[HeaderConsumerGroup] = p.group
	headers[HeaderLastError] = truncate(cause.Error(), maxErrorHeaderLen)

	result := outcomeRetried
	if queue == DLQName {
		headers[HeaderDeadLetteredReason] = deadLetterReason
		result = outcomeDeadLettered
	} else {
		headers[HeaderRetryCount] = int64(retryCount(d.Headers) + 1)
	}

	msg := amqp.Publishing{
		Headers:      headers,
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Timestamp:    d.Timestamp,
		Type:         d.Type,
		Body:         d.Body,
	}

	ctx, cancel := p.moveCtx()
	defer cancel()
	if err := p.pub.publish(ctx, "", queue, msg); err != nil {
		if nackErr := d.Nack(false, true); nackErr != nil {
			return outcomeRequeued, fmt.Errorf("move to %s: %w (nack: %v)", queue, err, nackErr)
		}
		return outcomeRequeued, fmt.Errorf("move to %s: %w", queue, err)
	}
	return result, d.Ack(false)
}

// retryCount reads the retry-count header; a missing or malformed header is 0.
func retryCount(headers amqp.Table) int {
	switch v := headers[HeaderRetryCount].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	default:
		return 0
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
