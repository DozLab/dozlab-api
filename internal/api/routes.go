package api

import (
	"log"
	"net/http"
	"os"

	"dozlab-backend/internal/api/handlers"
	"dozlab-backend/internal/audit"
	"dozlab-backend/internal/authz"
	"dozlab-backend/internal/database"
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
	auditHandler := handlers.NewAuditHandler(db)
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

	// Access control: each route names the permission it needs (internal/authz has the table of
	// which role has which). A request without it gets 403.
	require := middleware.RequirePermission
	// Reads of other people's data are recorded in the audit log even when they succeed
	sensitiveRead := func(c *gin.Context) { audit.Annotate(c).Always = true }

	// API v1 routes
	v1 := router.Group("/api/v1")
	// Audit log: every change, every refused or failed request, and the sensitive reads. It is
	// first so that requests refused for a bad token are recorded too. AUDIT_READS=true also
	// records every successful read.
	v1.Use(audit.Middleware(audit.NewDBRecorder(db), audit.Options{Reads: os.Getenv("AUDIT_READS") == "true"}))
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
		v1.GET("/ws", middleware.WebSocketAuthMiddleware(os.Getenv("JWT_SECRET")), middleware.CurrentUser(db), wsManager.HandleWebSocket)

		// Protected routes (require authentication)
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware(os.Getenv("JWT_SECRET")))
		// The user's role and status as they are now, not as they were when the token was issued
		protected.Use(middleware.CurrentUser(db))
		{
			// User routes
			users := protected.Group("/users")
			{
				users.GET("/profile", require(authz.ProfileRead), userHandler.GetProfile)
				users.PUT("/profile", require(authz.ProfileUpdate), userHandler.UpdateProfile)
				users.POST("/logout", authHandler.Logout)
			}

			// Lab routes
			labs := protected.Group("/labs")
			{
				labs.GET("/", require(authz.LabsRead), labHandler.GetLabs)
				labs.POST("/", require(authz.LabsCreate), labHandler.CreateLab)
				labs.GET("/:labId", require(authz.LabsRead), labHandler.GetLab)
				// Own labs; labs:manage_any (admins) lifts that in the handler
				labs.PUT("/:labId", require(authz.LabsUpdate), labHandler.UpdateLab)
				labs.DELETE("/:labId", require(authz.LabsDelete), labHandler.DeleteLab)
				
				// Lab specifications routes with composite key support
				labs.GET("/:labId/specs", require(authz.LabSpecsRead), labHandler.GetLabSpecs)
				labs.POST("/:labId/specs", require(authz.LabSpecsWrite), labHandler.CreateLabSpec)
				labs.GET("/:labId/specs/:version", require(authz.LabSpecsRead), labHandler.GetLabSpec)
				labs.PUT("/:labId/specs/:version", require(authz.LabSpecsWrite), labHandler.UpdateLabSpec)
				labs.DELETE("/:labId/specs/:version", require(authz.LabSpecsWrite), labHandler.DeleteLabSpec)
			}

			// Session routes (legacy - for backwards compatibility)
			sessions := protected.Group("/sessions")
			{
				sessions.GET("/", require(authz.SessionsRead), labHandler.GetSessions)
				sessions.POST("/", require(authz.SessionsCreate), labHandler.CreateSession)
				sessions.GET("/:id", require(authz.SessionsRead), labHandler.GetSession)
				// The handler sets the status of any session, so only admins may call it
				sessions.PUT("/:id/status", require(authz.SessionsManageAny), labHandler.UpdateSessionStatus)
				sessions.DELETE("/:id", require(authz.SessionsDelete), labHandler.DeleteSession)
			}

			// CRD-based lab session routes (new)
			if labSessionHandler != nil {
				labSessions := protected.Group("/lab-sessions")
				{
					// sessions:set_options is checked in the handler, when a request sets one
					labSessions.POST("/", require(authz.SessionsCreate), labSessionHandler.CreateLabSession)
					labSessions.GET("/", require(authz.SessionsRead), labSessionHandler.ListLabSessions)
					labSessions.GET("/:id", require(authz.SessionsRead), labSessionHandler.GetLabSession)
					labSessions.DELETE("/:id", require(authz.SessionsDelete), labSessionHandler.DeleteLabSession)
				}
			}

			// Host capacity check against lab session resource requests
			protected.POST("/host-check", require(authz.HostCheckRun), hostCheckHandler.CheckHost)

			// Progress routes
			progress := protected.Group("/progress")
			{
				progress.GET("/", require(authz.ProgressRead), userHandler.GetUserProgress)
				progress.GET("/labs/:labId", require(authz.ProgressRead), userHandler.GetLabProgress)
			}

			// Proxy endpoints for other microservices (optional - for convenience)
			// Note: Direct service-to-service communication should be preferred
			proxy := protected.Group("/proxy")
			{
				// WebSocket service proxy
				proxy.GET("/websocket/stats", require(authz.ServiceStatsRead), func(c *gin.Context) {
					stats, err := serviceClients.GetWebSocketStats(c.Request.Context())
					if err != nil {
						c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WebSocket service unavailable"})
						return
					}
					c.JSON(http.StatusOK, stats)
				})
				
				// Notifications go out on the event bus (RabbitMQ, routing key "notification")
				proxy.POST("/notifications", require(authz.NotificationsSend), notificationHandler.SendNotification)
			}
		}

		// Admin routes: each permission here is held by admins only
		admin := v1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(os.Getenv("JWT_SECRET")))
		admin.Use(middleware.CurrentUser(db))
		{
			admin.GET("/users", sensitiveRead, require(authz.UsersList), userHandler.GetAllUsers)
			admin.PUT("/users/:id/role", require(authz.UsersUpdateRole), userHandler.UpdateUserRole)
			admin.PUT("/users/:id/status", require(authz.UsersUpdateStatus), userHandler.UpdateUserStatus)
			// Reading the audit log is itself recorded
			admin.GET("/audit-logs", sensitiveRead, require(authz.AuditRead), auditHandler.ListAuditLogs)
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