package container

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"dozlab-backend/internal/config"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/messaging"
	"dozlab-backend/internal/services"
	"dozlab-backend/internal/websocket"
	"k8s.io/client-go/dynamic"
)

// Container holds all service dependencies
type Container struct {
	Config   *config.Config
	Database *database.Database
	
	// Services
	LabService     services.LabService
	UserService    services.UserService
	AuthService    services.AuthService
	SessionService services.SessionService
	EventBus       services.EventBusService
	ValidationService services.ValidationService
	
	// External clients
	K8sClient    dynamic.Interface
	WSManager    *websocket.Manager
	WSService    *websocket.SessionService
	// RedisEventBus stays for event storage (GetEvent) and session event streams
	RedisEventBus *websocket.RedisEventBus
	// RabbitEventBus carries EventBus publish/subscribe
	RabbitEventBus *messaging.RabbitEventBus
}

// eventConsumerGroup names this service's RabbitMQ queue (dozlab.events.dozlab-api)
const eventConsumerGroup = "dozlab-api"

// NewContainer creates and wires up all dependencies
func NewContainer(cfg *config.Config, db *database.Database, k8sClient dynamic.Interface) (*Container, error) {
	// Initialize Redis event bus with proper error handling
	redisEventBus, err := websocket.NewRedisEventBus(redisURL(cfg))
	if err != nil {
		return nil, err
	}

	// Initialize WebSocket components
	wsManager := websocket.NewManager()
	go wsManager.Start()
	wsService := websocket.NewSessionService(wsManager)

	// Create validation service
	validationService := services.NewValidationService(cfg)

	// RabbitMQ carries publish/subscribe; Redis keeps stored events
	rabbitEventBus, err := messaging.NewRabbitEventBus(messaging.Config{
		URL:           cfg.RabbitMQ.URL,
		ConsumerGroup: eventConsumerGroup,
		Prefetch:      cfg.RabbitMQ.Prefetch,
		MaxRetries:    cfg.RabbitMQ.MaxRetries,
		RetryDelay:    cfg.RabbitMQ.RetryDelay,
	})
	if err != nil {
		redisEventBus.Close()
		return nil, err
	}

	// Initialize event bus service first (needed by other services)
	eventBusService := services.NewEventBusService(rabbitEventBus, redisEventBus)

	// Initialize services with dependency injection
	labService := services.NewLabService(db)
	userService := services.NewUserService(db, eventBusService)
	authService := services.NewAuthService(db, eventBusService, cfg.JWTSecret)
	sessionService := services.NewSessionService(db, eventBusService)

	container := &Container{
		Config:   cfg,
		Database: db,
		
		// Services
		LabService:        labService,
		UserService:       userService,
		AuthService:       authService,
		SessionService:    sessionService,
		EventBus:          eventBusService,
		ValidationService: validationService,
		
		// External clients
		K8sClient:     k8sClient,
		WSManager:     wsManager,
		WSService:     wsService,
		RedisEventBus:  redisEventBus,
		RabbitEventBus: rabbitEventBus,
	}

	return container, nil
}

// StartEventConsumers subscribes this service's consumers to the event bus:
// notifications and LabSession phase changes (from dozlab-controller) go to the
// users' WebSocket connections. Each replica holds its own connections, so they
// use this process's instance queue (every replica gets every event) rather
// than the shared group queue.
func (c *Container) StartEventConsumers(ctx context.Context) error {
	c.RabbitEventBus.RegisterHandler(websocket.EventNotification, func(ctx context.Context, e *websocket.Event) error {
		return c.WSManager.HandleNotificationEvent(ctx, e)
	})
	c.RabbitEventBus.RegisterHandler(websocket.EventLabSessionPhaseChanged, func(ctx context.Context, e *websocket.Event) error {
		return c.WSManager.HandleLabSessionPhaseEvent(ctx, e)
	})
	return c.RabbitEventBus.SubscribeInstance(ctx, websocket.EventNotification, websocket.EventLabSessionPhaseChanged)
}

// redisURL returns cfg.Redis.URL, or builds one from RedisAddr/RedisPassword/RedisDB
func redisURL(cfg *config.Config) string {
	if cfg.Redis.URL != "" {
		return cfg.Redis.URL
	}
	u := url.URL{Scheme: "redis", Host: cfg.RedisAddr, Path: "/" + strconv.Itoa(cfg.RedisDB)}
	if cfg.RedisPassword != "" {
		u.User = url.UserPassword("", cfg.RedisPassword)
	}
	return u.String()
}

// Close gracefully shuts down all services
func (c *Container) Close() error {
	var errs []error
	if c.RabbitEventBus != nil {
		errs = append(errs, c.RabbitEventBus.Close())
	}
	if c.RedisEventBus != nil {
		errs = append(errs, c.RedisEventBus.Close())
	}
	return errors.Join(errs...)
}