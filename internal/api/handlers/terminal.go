package handlers

import (
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development - restrict in production
	},
}

// TerminalHandler handles terminal WebSocket connections
type TerminalHandler struct {
}

// NewTerminalHandler creates a new terminal handler
func NewTerminalHandler() *TerminalHandler {
	return &TerminalHandler{}
}

// HandleTerminalWebSocket proxies WebSocket connections to the terminal sidecar
// @Summary Connect to lab terminal
// @Description Establish WebSocket connection to lab terminal sidecar
// @Tags terminal
// @Param session_id query string true "Session ID"
// @Success 101 "WebSocket connection established"
// @Failure 400 {object} map[string]interface{} "Invalid session ID"
// @Failure 500 {object} map[string]interface{} "Connection failed"
// @Router /api/v1/terminal [get]
func (h *TerminalHandler) HandleTerminalWebSocket(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Session ID is required",
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User not authenticated",
		})
		return
	}

	// Upgrade to WebSocket
	clientConn, err := terminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	log.Printf("Terminal WebSocket connected for user %s, session %s", userID, sessionID)

	// Connect to terminal sidecar
	err = h.proxyToTerminalSidecar(clientConn, sessionID, userID.(string))
	if err != nil {
		log.Printf("Terminal proxy error: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("Terminal connection failed: "+err.Error()))
	}
}

// proxyToTerminalSidecar establishes connection to terminal sidecar and proxies messages
func (h *TerminalHandler) proxyToTerminalSidecar(clientConn *websocket.Conn, sessionID, userID string) error {
	// Build terminal sidecar URL
	sidecarURL := url.URL{
		Scheme:   "ws",
		Host:     "lab-service-" + sessionID + ":8081", // Kubernetes service name
		Path:     "/terminal",
		RawQuery: "session_id=" + sessionID,
	}

	log.Printf("Connecting to terminal sidecar: %s", sidecarURL.String())

	// Connect to terminal sidecar
	sidecarConn, _, err := websocket.DefaultDialer.Dial(sidecarURL.String(), nil)
	if err != nil {
		return err
	}
	defer sidecarConn.Close()

	// Set up bidirectional proxy
	done := make(chan error, 2)

	// Client -> Sidecar
	go func() {
		defer func() {
			sidecarConn.Close()
			done <- nil
		}()
		
		for {
			messageType, message, err := clientConn.ReadMessage()
			if err != nil {
				log.Printf("Error reading from client: %v", err)
				return
			}

			err = sidecarConn.WriteMessage(messageType, message)
			if err != nil {
				log.Printf("Error writing to sidecar: %v", err)
				return
			}
		}
	}()

	// Sidecar -> Client
	go func() {
		defer func() {
			clientConn.Close()
			done <- nil
		}()
		
		for {
			messageType, message, err := sidecarConn.ReadMessage()
			if err != nil {
				log.Printf("Error reading from sidecar: %v", err)
				return
			}

			err = clientConn.WriteMessage(messageType, message)
			if err != nil {
				log.Printf("Error writing to client: %v", err)
				return
			}
		}
	}()

	// Wait for either side to disconnect
	<-done
	log.Printf("Terminal proxy session ended for session %s", sessionID)
	return nil
}

// GetTerminalStatus gets the status of a terminal session
// @Summary Get terminal session status
// @Description Get the current status of a terminal session
// @Tags terminal
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Success 200 {object} map[string]interface{} "Terminal status"
// @Failure 404 {object} map[string]interface{} "Session not found"
// @Failure 500 {object} map[string]interface{} "Failed to get status"
// @Router /api/v1/terminal/{session_id}/status [get]
func (h *TerminalHandler) GetTerminalStatus(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID is required",
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

	// TODO: Check if session exists and user has access
	// For now, return basic status
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"session_id":     sessionID,
			"user_id":        userID,
			"status":         "active",
			"terminal_url":   "/api/v1/terminal?session_id=" + sessionID,
			"sidecar_health": "healthy", // TODO: Add actual health check
		},
	})
}

// SendTerminalCommand sends a command to the terminal session
// @Summary Send command to terminal
// @Description Send a command directly to the terminal session
// @Tags terminal
// @Accept json
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param command body object true "Command to send"
// @Success 200 {object} map[string]interface{} "Command sent"
// @Failure 400 {object} map[string]interface{} "Invalid command"
// @Failure 404 {object} map[string]interface{} "Session not found"
// @Router /api/v1/terminal/{session_id}/command [post]
func (h *TerminalHandler) SendTerminalCommand(c *gin.Context) {
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
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid command format: " + err.Error(),
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

	// TODO: Send command to terminal sidecar via HTTP API
	// For now, return success
	log.Printf("User %s sent command to session %s: %s", userID, sessionID, req.Command)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Command sent to terminal",
		"data": gin.H{
			"session_id": sessionID,
			"command":    req.Command,
			"timestamp":  "2024-01-01T00:00:00Z", // TODO: Add real timestamp
		},
	})
}