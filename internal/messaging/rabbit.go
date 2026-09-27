package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"dozlab-backend/internal/websocket"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultPublishTimeout = 10 * time.Second
	initialBackoff        = 500 * time.Millisecond
	maxBackoff            = 30 * time.Second
)

// ErrNotConnected is returned while the bus is reconnecting to RabbitMQ.
var ErrNotConnected = errors.New("rabbitmq: not connected")

// ErrClosed is returned after Close.
var ErrClosed = errors.New("rabbitmq: event bus closed")

// Config configures a RabbitEventBus.
type Config struct {
	URL string
	// ConsumerGroup names this service's queue; instances in one group share the work.
	ConsumerGroup string
	// Prefetch is the number of unacked deliveries per consumer.
	Prefetch int
	// MaxRetries is how many times a failed message is retried before it goes to the DLQ.
	MaxRetries int
	// RetryDelay is how long a failed message waits before it is redelivered.
	RetryDelay time.Duration
	// PublishTimeout bounds a publish (including its confirm) when the caller's
	// context has no deadline. Defaults to 10s.
	PublishTimeout time.Duration
	// InstanceID names this process's instance queue (SubscribeInstance).
	// Defaults to <hostname>-<random>, so every process gets its own queue.
	InstanceID string
}

func (c Config) validate() error {
	var errs []error
	if c.URL == "" {
		errs = append(errs, errors.New("URL is required"))
	}
	if c.ConsumerGroup == "" {
		errs = append(errs, errors.New("consumer group is required"))
	}
	if c.Prefetch < 1 {
		errs = append(errs, errors.New("prefetch must be at least 1"))
	}
	if c.MaxRetries < 0 {
		errs = append(errs, errors.New("max retries must not be negative"))
	}
	if c.RetryDelay <= 0 {
		errs = append(errs, errors.New("retry delay must be positive"))
	}
	return errors.Join(errs...)
}

// RabbitEventBus publishes and consumes websocket.Events through RabbitMQ.
// It reconnects with backoff when the connection or a channel drops.
type RabbitEventBus struct {
	cfg Config

	ctx    context.Context // canceled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup // supervisor and consumer goroutines

	mu                sync.RWMutex
	handlers          map[websocket.EventType][]websocket.EventHandler
	eventTypes        map[string]bool // event types bound to this group's queue
	instanceTypes     map[string]bool // event types bound to this instance's queue
	consuming         bool
	instanceConsuming bool
	closed            bool

	connMu sync.Mutex
	conn   *amqp.Connection
	pubCh  *amqp.Channel
}

// NewRabbitEventBus connects to RabbitMQ and declares the exchange and DLQ.
// The first connection must succeed; later drops are retried in the background.
func NewRabbitEventBus(cfg Config) (*RabbitEventBus, error) {
	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = defaultPublishTimeout
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = defaultInstanceID()
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("rabbitmq config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	bus := &RabbitEventBus{
		cfg:           cfg,
		ctx:           ctx,
		cancel:        cancel,
		handlers:      make(map[websocket.EventType][]websocket.EventHandler),
		eventTypes:    make(map[string]bool),
		instanceTypes: make(map[string]bool),
	}

	conn, err := bus.connect()
	if err != nil {
		cancel()
		return nil, err
	}

	bus.wg.Add(1)
	go bus.supervise(conn)
	return bus, nil
}

// defaultInstanceID is <hostname>-<8 random hex chars>. The hostname (the pod name
// in Kubernetes) makes the queue recognisable; the suffix keeps two processes on
// one host, or a restarted process, from sharing a queue.
func defaultInstanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "instance"
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Sprintf("%s-%d", host, time.Now().UnixNano())
	}
	return host + "-" + hex.EncodeToString(suffix)
}

// InstanceQueue is the name of this process's instance queue.
func (b *RabbitEventBus) InstanceQueue() string {
	return InstanceQueueName(b.cfg.ConsumerGroup, b.cfg.InstanceID)
}

// connect dials RabbitMQ and declares the base topology.
func (b *RabbitEventBus) connect() (*amqp.Connection, error) {
	conn, err := amqp.Dial(b.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	err = declareBase(ch)
	ch.Close()
	if err != nil {
		conn.Close()
		return nil, err
	}

	b.connMu.Lock()
	b.conn = conn
	b.pubCh = nil // reopened lazily on the new connection
	b.connMu.Unlock()
	return conn, nil
}

// supervise reconnects with backoff whenever the connection drops.
func (b *RabbitEventBus) supervise(conn *amqp.Connection) {
	defer b.wg.Done()
	for {
		closed := conn.NotifyClose(make(chan *amqp.Error, 1))
		select {
		case <-b.ctx.Done():
			return
		case amqpErr := <-closed:
			if b.ctx.Err() != nil {
				return
			}
			log.Printf("rabbitmq: connection lost: %v; reconnecting", amqpErr)
		}

		backoff := initialBackoff
		for {
			if !b.sleep(backoff) {
				return
			}
			var err error
			if conn, err = b.connect(); err == nil {
				log.Printf("rabbitmq: reconnected")
				break
			}
			log.Printf("rabbitmq: reconnect failed: %v", err)
			backoff = nextBackoff(backoff)
		}
	}
}

// sleep waits d, returning false if the bus is closed first.
func (b *RabbitEventBus) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-b.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	if d *= 2; d > maxBackoff {
		return maxBackoff
	}
	return d
}

// publishChannel returns the confirm-mode publishing channel, reopening it if needed.
func (b *RabbitEventBus) publishChannel() (*amqp.Channel, error) {
	b.connMu.Lock()
	defer b.connMu.Unlock()
	if b.ctx.Err() != nil {
		return nil, ErrClosed
	}
	if b.pubCh != nil && !b.pubCh.IsClosed() {
		return b.pubCh, nil
	}
	if b.conn == nil || b.conn.IsClosed() {
		return nil, ErrNotConnected
	}
	ch, err := b.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publish channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	b.pubCh = ch
	return ch, nil
}

// publish sends msg persistently and waits for the broker's confirm.
func (b *RabbitEventBus) publish(ctx context.Context, exchange, key string, msg amqp.Publishing) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.cfg.PublishTimeout)
		defer cancel()
	}
	ch, err := b.publishChannel()
	if err != nil {
		return err
	}
	msg.DeliveryMode = amqp.Persistent
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, key, false, false, msg)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for publisher confirm: %w", err)
	}
	if !acked {
		return errors.New("publish: broker did not confirm the message")
	}
	return nil
}

// Publish publishes an event to dozlab.events with its type as the routing key.
func (b *RabbitEventBus) Publish(ctx context.Context, event *websocket.Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return b.publish(ctx, ExchangeName, string(event.Type), amqp.Publishing{
		ContentType: "application/json",
		MessageId:   event.ID,
		Timestamp:   event.Timestamp,
		Type:        string(event.Type),
		Body:        body,
	})
}

// RegisterHandler adds a handler for an event type. All handlers for a type
// must succeed for a message to be acked; on failure they all run again on retry,
// so handlers should be idempotent.
func (b *RabbitEventBus) RegisterHandler(eventType websocket.EventType, handler websocket.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Subscribe binds the group's queue to eventTypes and starts consuming. When it
// returns nil the bindings exist, so events published afterwards are queued.
// The types are remembered even if binding fails (e.g. while reconnecting) and
// are bound again when the consumer (re)starts. Handlers run with a context
// that is canceled by Close.
func (b *RabbitEventBus) Subscribe(ctx context.Context, eventTypes ...websocket.EventType) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	keys, err := addTypes(b.eventTypes, b.instanceTypes, eventTypes)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	startConsumer := !b.consuming
	b.consuming = true
	if startConsumer {
		// Added under mu so it cannot race with Close's Wait.
		b.wg.Add(1)
	}
	b.mu.Unlock()

	sub := b.groupSubscription()
	err = b.bind(sub, keys)
	if startConsumer {
		go b.runConsumer(sub)
	}
	return err
}

// SubscribeInstance binds this process's own queue (InstanceQueue) to
// eventTypes and starts consuming it. Unlike Subscribe, where the instances of
// a group share one queue and each event reaches one of them, every instance
// receives every event of these types. Use it for events that must reach state
// held by each process, such as its WebSocket connections. A failed event goes
// to the DLQ without retries. An event type can't be both group- and
// instance-subscribed, since its handlers would then run twice.
func (b *RabbitEventBus) SubscribeInstance(ctx context.Context, eventTypes ...websocket.EventType) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	keys, err := addTypes(b.instanceTypes, b.eventTypes, eventTypes)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	startConsumer := !b.instanceConsuming
	b.instanceConsuming = true
	if startConsumer {
		b.wg.Add(1)
	}
	b.mu.Unlock()

	sub := b.instanceSubscription()
	err = b.bind(sub, keys)
	if startConsumer {
		go b.runConsumer(sub)
	}
	return err
}

// addTypes records eventTypes in dst, refusing any already in other.
// Callers hold b.mu.
func addTypes(dst, other map[string]bool, eventTypes []websocket.EventType) ([]string, error) {
	keys := make([]string, 0, len(eventTypes))
	for _, t := range eventTypes {
		if other[string(t)] {
			return nil, fmt.Errorf("event type %q is already subscribed on the other queue (group vs instance)", t)
		}
		keys = append(keys, string(t))
	}
	for _, k := range keys {
		dst[k] = true
	}
	return keys, nil
}

// subscription is a queue this bus consumes: the group queue or the instance queue.
type subscription struct {
	name       string // for logs
	queue      string
	maxRetries int // 0: a failed message goes straight to the DLQ
	// declare declares the queue and binds it to keys.
	declare func(ch declarer, keys []string) error
	// types returns every event type bound so far, to re-declare on reconnect.
	types func() []string
}

func (b *RabbitEventBus) groupSubscription() subscription {
	return subscription{
		name:       "group " + b.cfg.ConsumerGroup,
		queue:      QueueName(b.cfg.ConsumerGroup),
		maxRetries: b.cfg.MaxRetries,
		declare: func(ch declarer, keys []string) error {
			return declareGroup(ch, b.cfg.ConsumerGroup, keys, b.cfg.RetryDelay)
		},
		types: func() []string { return b.sortedTypes(b.eventTypes) },
	}
}

func (b *RabbitEventBus) instanceSubscription() subscription {
	queue := b.InstanceQueue()
	return subscription{
		name:       "instance " + b.cfg.InstanceID,
		queue:      queue,
		maxRetries: 0,
		declare: func(ch declarer, keys []string) error {
			return declareInstance(ch, queue, keys)
		},
		types: func() []string { return b.sortedTypes(b.instanceTypes) },
	}
}

// bind declares sub's queue and binds keys on a short-lived channel.
func (b *RabbitEventBus) bind(sub subscription, keys []string) error {
	b.connMu.Lock()
	conn := b.conn
	b.connMu.Unlock()
	if conn == nil || conn.IsClosed() {
		return ErrNotConnected
	}
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()
	return sub.declare(ch, keys)
}

func (b *RabbitEventBus) sortedTypes(types map[string]bool) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dispatch runs every handler registered for the event's type.
func (b *RabbitEventBus) dispatch(ctx context.Context, event *websocket.Event) error {
	b.mu.RLock()
	handlers := append([]websocket.EventHandler(nil), b.handlers[event.Type]...)
	b.mu.RUnlock()

	var errs []error
	for _, h := range handlers {
		if err := h(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// runConsumer consumes sub's queue until Close, restarting with backoff
// when the channel or connection drops.
func (b *RabbitEventBus) runConsumer(sub subscription) {
	defer b.wg.Done()
	p := &processor{
		group:      b.cfg.ConsumerGroup,
		maxRetries: sub.maxRetries,
		dispatch:   b.dispatch,
		pub:        b,
		moveCtx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), b.cfg.PublishTimeout)
		},
	}

	backoff := initialBackoff
	for {
		started, err := b.consumeOnce(sub, p)
		if b.ctx.Err() != nil {
			return
		}
		if started {
			backoff = initialBackoff
		}
		log.Printf("rabbitmq: %s consumer stopped: %v; restarting in %s", sub.name, err, backoff)
		if !b.sleep(backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// consumeOnce opens a channel, declares sub's queue and processes deliveries
// until the channel closes. started reports whether consuming began.
func (b *RabbitEventBus) consumeOnce(sub subscription, p *processor) (started bool, err error) {
	b.connMu.Lock()
	conn := b.conn
	b.connMu.Unlock()
	if conn == nil || conn.IsClosed() {
		return false, ErrNotConnected
	}

	ch, err := conn.Channel()
	if err != nil {
		return false, fmt.Errorf("open consumer channel: %w", err)
	}
	defer ch.Close()

	if err := ch.Qos(b.cfg.Prefetch, 0, false); err != nil {
		return false, fmt.Errorf("set prefetch: %w", err)
	}
	if err := sub.declare(ch, sub.types()); err != nil {
		return false, err
	}
	deliveries, err := ch.ConsumeWithContext(b.ctx, sub.queue, "", false, false, false, false, nil)
	if err != nil {
		return false, fmt.Errorf("consume: %w", err)
	}

	// Prefetch bounds the deliveries in flight, so one goroutine each is fine.
	var inFlight sync.WaitGroup
	for d := range deliveries {
		inFlight.Add(1)
		go func(d amqp.Delivery) {
			defer inFlight.Done()
			if result, err := p.process(b.ctx, d); err != nil {
				log.Printf("rabbitmq: delivery %s (outcome %d): %v", d.MessageId, result, err)
			}
		}(d)
	}
	inFlight.Wait()
	return true, errors.New("delivery channel closed")
}

// Close stops consuming, waits for in-flight handlers and closes the connection.
func (b *RabbitEventBus) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.cancel()
	b.wg.Wait()

	b.connMu.Lock()
	defer b.connMu.Unlock()
	var errs []error
	if b.pubCh != nil && !b.pubCh.IsClosed() {
		errs = append(errs, b.pubCh.Close())
	}
	if b.conn != nil && !b.conn.IsClosed() {
		errs = append(errs, b.conn.Close())
	}
	b.pubCh, b.conn = nil, nil
	return errors.Join(errs...)
}
