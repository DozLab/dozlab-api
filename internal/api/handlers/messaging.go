package handlers

import (
	"net/http"
	"strconv"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MessagingHandler handles messaging and event-related API endpoints
type MessagingHandler struct {
	db       *database.Database
	cfg      *config.Config
	eventBus *websocket.RedisEventBus
}

// NewMessagingHandler creates a new messaging handler
func NewMessagingHandler(db *database.Database, cfg *config.Config, eventBus *websocket.RedisEventBus) *MessagingHandler {
	return &MessagingHandler{
		db:       db,
		cfg:      cfg,
		eventBus: eventBus,
	}
}

// PublishEvent publishes an event to the event bus
// @Summary Publish event
// @Description Publish an event to the distributed event system
// @Tags messaging
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Event data"
// @Success 200 {object} map[string]interface{} "Event published successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to publish event"
// @Router /api/v1/events/publish [post]
func (h *MessagingHandler) PublishEvent(c *gin.Context) {
	var req struct {
		Type      websocket.EventType        `json:"type" binding:"required"`
		SessionID string                     `json:"session_id,omitempty"`
		LabID     string                     `json:"lab_id,omitempty"`
		Data      map[string]interface{}     `json:"data"`
		TTL       *int                       `json:"ttl,omitempty"` // TTL in seconds
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Create event
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      req.Type,
		Source:    "api",
		SessionID: req.SessionID,
		UserID:    userID.(string),
		LabID:     req.LabID,
		Data:      req.Data,
		Timestamp: time.Now(),
	}

	if req.TTL != nil {
		event.TTL = time.Duration(*req.TTL) * time.Second
	}

	// Publish event
	if err := h.eventBus.Publish(c.Request.Context(), event); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to publish event: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "Event published successfully",
		"event_id": event.ID,
	})
}

// GetEvent retrieves a specific event by ID
// @Summary Get event
// @Description Get a specific event by its ID
// @Tags messaging
// @Produce json
// @Security Bearer
// @Param event_id path string true "Event ID"
// @Success 200 {object} map[string]interface{} "Event details"
// @Failure 404 {object} map[string]interface{} "Event not found"
// @Failure 500 {object} map[string]interface{} "Failed to get event"
// @Router /api/v1/events/{event_id} [get]
func (h *MessagingHandler) GetEvent(c *gin.Context) {
	eventID := c.Param("event_id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Event ID is required",
		})
		return
	}

	event, err := h.eventBus.GetEvent(c.Request.Context(), eventID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "event not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to get event: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    event,
	})
}

// ListEvents lists recent events with optional filtering
// @Summary List events
// @Description List recent events with optional filtering by type
// @Tags messaging
// @Produce json
// @Security Bearer
// @Param types query string false "Comma-separated event types to filter by"
// @Param limit query int false "Maximum number of events to return" default(50)
// @Success 200 {array} map[string]interface{} "List of events"
// @Failure 500 {object} map[string]interface{} "Failed to list events"
// @Router /api/v1/events [get]
func (h *MessagingHandler) ListEvents(c *gin.Context) {
	// Parse query parameters
	typesParam := c.Query("types")
	limitParam := c.DefaultQuery("limit", "50")

	limit, err := strconv.Atoi(limitParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid limit parameter",
		})
		return
	}

	var eventTypes []websocket.EventType
	if typesParam != "" {
		// Parse comma-separated event types
		typeStrings := splitAndTrim(typesParam, ",")
		for _, typeStr := range typeStrings {
			eventTypes = append(eventTypes, websocket.EventType(typeStr))
		}
	}

	events, err := h.eventBus.ListEvents(c.Request.Context(), eventTypes, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to list events: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    events,
		"count":   len(events),
	})
}

// GetSessionEvents gets events for a specific session
// @Summary Get session events
// @Description Get all events for a specific session from the event stream
// @Tags messaging
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param start query string false "Start ID for pagination (default: beginning)"
// @Param count query int false "Number of events to return" default(100)
// @Success 200 {array} map[string]interface{} "List of session events"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to get session events"
// @Router /api/v1/events/sessions/{session_id} [get]
func (h *MessagingHandler) GetSessionEvents(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
		})
		return
	}

	start := c.Query("start")
	countParam := c.DefaultQuery("count", "100")

	count, err := strconv.ParseInt(countParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid count parameter",
		})
		return
	}

	events, err := h.eventBus.GetSessionEvents(c.Request.Context(), sessionID, start, count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get session events: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    events,
		"count":   len(events),
	})
}

// GetEventMetrics gets metrics about the event system
// @Summary Get event metrics
// @Description Get metrics and statistics about the event system
// @Tags messaging
// @Produce json
// @Security Bearer
// @Success 200 {object} map[string]interface{} "Event system metrics"
// @Failure 500 {object} map[string]interface{} "Failed to get metrics"
// @Router /api/v1/events/metrics [get]
func (h *MessagingHandler) GetEventMetrics(c *gin.Context) {
	metrics, err := h.eventBus.GetMetrics(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get event metrics: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    metrics,
	})
}

// CreateEventPattern creates a pattern-based event subscription (WebSocket endpoint)
// @Summary Create event pattern subscription
// @Description Create a pattern-based subscription for real-time event streaming
// @Tags messaging
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Pattern subscription request"
// @Success 200 {object} map[string]interface{} "Pattern subscription created"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to create subscription"
// @Router /api/v1/events/subscribe [post]
func (h *MessagingHandler) CreateEventPattern(c *gin.Context) {
	var req struct {
		Pattern string `json:"pattern" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// For now, return success - actual WebSocket subscription would be handled separately
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Event pattern subscription created (use WebSocket endpoint for real-time events)",
		"pattern": req.Pattern,
	})
}

// PublishLabEvent publishes a lab-specific event
// @Summary Publish lab event
// @Description Publish an event related to lab operations
// @Tags messaging
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Lab event data"
// @Success 200 {object} map[string]interface{} "Lab event published successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to publish event"
// @Router /api/v1/events/lab [post]
func (h *MessagingHandler) PublishLabEvent(c *gin.Context) {
	var req struct {
		Type      string                     `json:"type" binding:"required"` // started, stopped, completed, failed
		LabID     string                     `json:"lab_id" binding:"required"`
		SessionID string                     `json:"session_id" binding:"required"`
		Data      map[string]interface{}     `json:"data"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Map lab event type
	var eventType websocket.EventType
	switch req.Type {
	case "started":
		eventType = websocket.EventLabStarted
	case "stopped":
		eventType = websocket.EventLabStopped
	case "completed":
		eventType = websocket.EventLabCompleted
	case "failed":
		eventType = websocket.EventLabFailed
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid lab event type",
		})
		return
	}

	// Create and publish event
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		Source:    "lab-api",
		SessionID: req.SessionID,
		UserID:    userID.(string),
		LabID:     req.LabID,
		Data:      req.Data,
		Timestamp: time.Now(),
		TTL:       24 * time.Hour, // Lab events stored for 24 hours
	}

	if err := h.eventBus.Publish(c.Request.Context(), event); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to publish lab event: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "Lab event published successfully",
		"event_id": event.ID,
	})
}

// PublishFileEvent publishes a file system event
// @Summary Publish file event
// @Description Publish an event related to file system operations
// @Tags messaging
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "File event data"
// @Success 200 {object} map[string]interface{} "File event published successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to publish event"
// @Router /api/v1/events/file [post]
func (h *MessagingHandler) PublishFileEvent(c *gin.Context) {
	var req struct {
		Type      string                     `json:"type" binding:"required"` // created, modified, deleted, moved, shared
		SessionID string                     `json:"session_id" binding:"required"`
		FileID    string                     `json:"file_id,omitempty"`
		Path      string                     `json:"path,omitempty"`
		Data      map[string]interface{}     `json:"data"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Map file event type
	var eventType websocket.EventType
	switch req.Type {
	case "created":
		eventType = websocket.EventFileCreated
	case "modified":
		eventType = websocket.EventFileModified
	case "deleted":
		eventType = websocket.EventFileDeleted
	case "moved":
		eventType = websocket.EventFileMoved
	case "shared":
		eventType = websocket.EventFileShared
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid file event type",
		})
		return
	}

	// Ensure data contains file information
	if req.Data == nil {
		req.Data = make(map[string]interface{})
	}
	if req.FileID != "" {
		req.Data["file_id"] = req.FileID
	}
	if req.Path != "" {
		req.Data["path"] = req.Path
	}

	// Create and publish event
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		Source:    "api-service",
		SessionID: req.SessionID,
		UserID:    userID.(string),
		Data:      req.Data,
		Timestamp: time.Now(),
		TTL:       6 * time.Hour, // File events stored for 6 hours
	}

	if err := h.eventBus.Publish(c.Request.Context(), event); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to publish file event: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "File event published successfully",
		"event_id": event.ID,
	})
}

// Helper functions

func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	
	parts := make([]string, 0)
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	// Simple string split implementation
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

func trimString(s string) string {
	// Simple string trim implementation
	start := 0
	end := len(s)
	
	// Trim leading whitespace
	for start < end && isWhitespace(s[start]) {
		start++
	}
	
	// Trim trailing whitespace
	for end > start && isWhitespace(s[end-1]) {
		end--
	}
	
	return s[start:end]
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}