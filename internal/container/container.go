package container

import (
	"dozlab-backend/internal/database"
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
	RedisEventBus *websocket.RedisEventBus
}

// NewContainer creates and wires up all dependencies
func NewContainer(cfg *config.Config, db *database.Database, k8sClient dynamic.Interface) (*Container, error) {
	// Initialize Redis event bus with proper error handling
	redisEventBus, err := websocket.NewRedisEventBus(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		return nil, err
	}

	// Initialize WebSocket components
	wsManager := websocket.NewManager()
	wsService := websocket.NewSessionService(wsManager, redisEventBus)

	// Create validation service
	validationService := services.NewValidationService(cfg)

	// Initialize event bus service first (needed by other services)
	eventBusService := services.NewEventBusService(redisEventBus)

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
		RedisEventBus: redisEventBus,
	}

	return container, nil
}

// Close gracefully shuts down all services
func (c *Container) Close() error {
	if c.RedisEventBus != nil {
		if err := c.RedisEventBus.Close(); err != nil {
			return err
		}
	}
	return nil
}