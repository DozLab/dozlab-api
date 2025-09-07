package handlers

import (
	"net/http"

	"dozlab-backend/internal/database" 
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
)

// WebSocketHandler handles WebSocket-related API endpoints
type WebSocketHandler struct {
	db             *database.Database
	cfg            *config.Config
	wsManager      *websocket.Manager
	sessionService *websocket.SessionService
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(db *database.Database, cfg *config.Config, wsManager *websocket.Manager) *WebSocketHandler {
	return &WebSocketHandler{
		db:             db,
		cfg:            cfg,
		wsManager:      wsManager,
		sessionService: websocket.NewSessionService(wsManager),
	}
}

// GetWebSocketStats returns WebSocket connection statistics
// @Summary Get WebSocket connection statistics
// @Description Get current WebSocket connection statistics including connected users and sessions
// @Tags websocket
// @Produce json
// @Security Bearer
// @Success 200 {object} map[string]interface{} "WebSocket statistics"
// @Router /api/v1/ws/stats [get]
func (h *WebSocketHandler) GetWebSocketStats(c *gin.Context) {
	stats := h.wsManager.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// GetSessionConnections returns active connections for a specific session
// @Summary Get session connections
// @Description Get active WebSocket connections for a specific lab session
// @Tags websocket
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Success 200 {object} map[string]interface{} "Session connection info"
// @Router /api/v1/ws/sessions/{session_id}/connections [get]
func (h *WebSocketHandler) GetSessionConnections(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
		})
		return
	}

	stats := h.sessionService.GetSessionStats(sessionID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// SendSessionMessage sends a message to all clients in a session
// @Summary Send message to session
// @Description Send a message to all WebSocket clients connected to a specific lab session
// @Tags websocket
// @Accept json
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param request body object true "Message data"
// @Success 200 {object} map[string]interface{} "Message sent successfully"
// @Router /api/v1/ws/sessions/{session_id}/message [post]
func (h *WebSocketHandler) SendSessionMessage(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
		})
		return
	}

	var req struct {
		Type    string      `json:"type" binding:"required"`
		Title   string      `json:"title,omitempty"`
		Message string      `json:"message,omitempty"`
		Data    interface{} `json:"data,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	switch req.Type {
	case "notification":
		notificationType := "info"
		if req.Data != nil {
			if data, ok := req.Data.(map[string]interface{}); ok {
				if nt, exists := data["type"]; exists {
					if ntStr, ok := nt.(string); ok {
						notificationType = ntStr
					}
				}
			}
		}
		h.sessionService.SendSessionNotification(sessionID, req.Title, req.Message, notificationType)
	case "terminal":
		if req.Message != "" {
			h.sessionService.SendTerminalOutput(sessionID, req.Message)
		}
	case "error":
		h.sessionService.SendSessionError(sessionID, req.Message)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid message type. Supported types: notification, terminal, error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Message sent to session",
	})
}

// SendTerminalCommand sends a terminal command to a session
// @Summary Send terminal command to session
// @Description Send a terminal command to all clients in a lab session
// @Tags websocket
// @Accept json
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param request body object true "Command data"
// @Success 200 {object} map[string]interface{} "Command sent successfully"
// @Router /api/v1/ws/sessions/{session_id}/terminal [post]
func (h *WebSocketHandler) SendTerminalCommand(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
		})
		return
	}

	var req struct {
		Command string `json:"command" binding:"required"`
		Output  string `json:"output,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Send command execution result
	output := req.Output
	if output == "" {
		// Default output for demonstration
		output = "$ " + req.Command + "\nCommand executed successfully\n"
	}

	h.sessionService.SendTerminalOutput(sessionID, output)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Terminal command sent to session",
		"data": gin.H{
			"command": req.Command,
			"output":  output,
		},
	})
}

// UpdateSessionProgress updates progress for a lab session
// @Summary Update session progress
// @Description Update progress for a lab session and broadcast to all connected clients
// @Tags websocket
// @Accept json
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param request body object true "Progress data"
// @Success 200 {object} map[string]interface{} "Progress updated successfully"
// @Router /api/v1/ws/sessions/{session_id}/progress [put]
func (h *WebSocketHandler) UpdateSessionProgress(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
		})
		return
	}

	var req struct {
		LabID      string  `json:"lab_id" binding:"required"`
		UserID     string  `json:"user_id" binding:"required"`
		Progress   float64 `json:"progress" binding:"required,min=0,max=100"`
		TaskID     string  `json:"task_id,omitempty"`
		TaskStatus string  `json:"task_status,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Update session progress
	h.sessionService.UpdateSessionProgress(
		req.UserID,
		req.LabID,
		sessionID,
		req.Progress,
		req.TaskID,
		req.TaskStatus,
	)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Session progress updated",
		"data": gin.H{
			"session_id":  sessionID,
			"progress":    req.Progress,
			"task_id":     req.TaskID,
			"task_status": req.TaskStatus,
		},
	})
}