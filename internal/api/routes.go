package api

import (
	"log"
	"net/http"
	"os"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/api/handlers"
	"dozlab-backend/internal/middleware"
	"dozlab-backend/internal/services"
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func SetupRoutes(router *gin.Engine, db *database.Database, eventBus services.EventBusService, wsManager *websocket.Manager) {
	// Initialize service clients for microservices communication
	serviceConfig := services.ServiceConfig{
		WebSocketServiceURL:  os.Getenv("WEBSOCKET_SERVICE_URL"),
		ExaminerServiceURL:   os.Getenv("EXAMINER_SERVICE_URL"),
		WorkflowServiceURL:   os.Getenv("WORKFLOW_SERVICE_URL"),
		RedisAddr:           os.Getenv("REDIS_ADDR"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		RedisDB:             0, // Default to 0 for microservice
	}
	serviceClients := services.NewServiceClients(serviceConfig)

	// Initialize dynamic Kubernetes client for CRDs only
	var dynamicClient dynamic.Interface
	if config, err := getK8sConfig(); err == nil {
		if dc, err := dynamic.NewForConfig(config); err == nil {
			dynamicClient = dc
			log.Printf("Dynamic Kubernetes client initialized for CRDs")
		} else {
			log.Printf("Warning: Failed to initialize dynamic Kubernetes client: %v", err)
		}
	} else {
		log.Printf("Warning: Failed to get Kubernetes config: %v", err)
	}

	// Initialize handlers with service clients
	userHandler := handlers.NewUserHandler(db)
	labHandler := handlers.NewLabHandler(db)
	authHandler := handlers.NewAuthHandler(db)
	notificationHandler := handlers.NewNotificationHandler(eventBus)
	hostCheckHandler := handlers.NewHostCheckHandler()
	
	// Initialize CRD-based lab session handler
	var labSessionHandler *handlers.LabSessionHandler
	if dynamicClient != nil {
		labSessionHandler = handlers.NewLabSessionHandler(db, dynamicClient)
	}

	// Swagger documentation endpoint
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		if err := db.HealthCheck(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "Database connection failed",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"message": "Dozlab Backend is healthy",
		})
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Authentication routes (no auth required)
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
		}

		// WebSocket (notifications). Browsers can't set Authorization on a WebSocket, so they
		// send the JWT as a subprotocol: new WebSocket(url, ["dozlab.bearer", token]).
		// See docs/decision.md.
		v1.GET("/ws", middleware.WebSocketAuthMiddleware(os.Getenv("JWT_SECRET")), wsManager.HandleWebSocket)

		// Protected routes (require authentication)
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware(os.Getenv("JWT_SECRET")))
		{
			// User routes
			users := protected.Group("/users")
			{
				users.GET("/profile", userHandler.GetProfile)
				users.PUT("/profile", userHandler.UpdateProfile)
				users.POST("/logout", authHandler.Logout)
			}

			// Lab routes
			labs := protected.Group("/labs")
			{
				labs.GET("/", labHandler.GetLabs)
				labs.POST("/", labHandler.CreateLab)
				labs.GET("/:labId", labHandler.GetLab)
				labs.PUT("/:labId", labHandler.UpdateLab)
				labs.DELETE("/:labId", labHandler.DeleteLab)
				
				// Lab specifications routes with composite key support
				labs.GET("/:labId/specs", labHandler.GetLabSpecs)
				labs.POST("/:labId/specs", labHandler.CreateLabSpec)
				labs.GET("/:labId/specs/:version", labHandler.GetLabSpec)
				labs.PUT("/:labId/specs/:version", labHandler.UpdateLabSpec)
				labs.DELETE("/:labId/specs/:version", labHandler.DeleteLabSpec)
			}

			// Session routes (legacy - for backwards compatibility)
			sessions := protected.Group("/sessions")
			{
				sessions.GET("/", labHandler.GetSessions)
				sessions.POST("/", labHandler.CreateSession)
				sessions.GET("/:id", labHandler.GetSession)
				sessions.PUT("/:id/status", labHandler.UpdateSessionStatus)
				sessions.DELETE("/:id", labHandler.DeleteSession)
			}

			// CRD-based lab session routes (new)
			if labSessionHandler != nil {
				labSessions := protected.Group("/lab-sessions")
				{
					labSessions.POST("/", labSessionHandler.CreateLabSession)
					labSessions.GET("/", labSessionHandler.ListLabSessions)
					labSessions.GET("/:id", labSessionHandler.GetLabSession)
					labSessions.DELETE("/:id", labSessionHandler.DeleteLabSession)
				}
			}

			// Host capacity check against lab session resource requests
			protected.POST("/host-check", hostCheckHandler.CheckHost)

			// Progress routes
			progress := protected.Group("/progress")
			{
				progress.GET("/", userHandler.GetUserProgress)
				progress.GET("/labs/:labId", userHandler.GetLabProgress)
			}

			// Proxy endpoints for other microservices (optional - for convenience)
			// Note: Direct service-to-service communication should be preferred
			proxy := protected.Group("/proxy")
			{
				// WebSocket service proxy
				proxy.GET("/websocket/stats", func(c *gin.Context) {
					stats, err := serviceClients.GetWebSocketStats(c.Request.Context())
					if err != nil {
						c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WebSocket service unavailable"})
						return
					}
					c.JSON(http.StatusOK, stats)
				})
				
				// Notifications go out on the event bus (RabbitMQ, routing key "notification")
				proxy.POST("/notifications", notificationHandler.SendNotification)
			}
		}

		// Admin routes (require admin role)
		admin := v1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(os.Getenv("JWT_SECRET")))
		admin.Use(middleware.RoleMiddleware("admin"))
		{
			admin.GET("/users", userHandler.GetAllUsers)
			admin.PUT("/users/:id/role", userHandler.UpdateUserRole)
			admin.PUT("/users/:id/status", userHandler.UpdateUserStatus)
		}
	}
}

// getK8sConfig returns Kubernetes configuration
func getK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	
	// Fall back to kubeconfig
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}