package handlers

import (
	"context"
	"net/http"
	"time"

	"dozlab-backend/internal/services"
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NotificationPublisher is the part of services.EventBusService the handler uses.
type NotificationPublisher interface {
	PublishCriticalEvent(ctx context.Context, event interface{}) error
}

// NotificationHandler publishes notifications on the event bus.
type NotificationHandler struct {
	publisher NotificationPublisher
}

// NewNotificationHandler creates a new notification handler
func NewNotificationHandler(publisher NotificationPublisher) *NotificationHandler {
	return &NotificationHandler{publisher: publisher}
}

// SendNotification publishes a notification event
// @Summary Send notification
// @Description Publish a notification for a user on the event bus (routing key "notification")
// @Tags notifications
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body services.NotificationRequest true "Notification"
// @Success 200 {object} map[string]interface{} "Notification sent"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 503 {object} map[string]interface{} "Event bus unavailable"
// @Router /proxy/notifications [post]
func (h *NotificationHandler) SendNotification(c *gin.Context) {
	var req services.NotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	senderID, _ := c.Get("user_id")
	event := &websocket.Event{
		ID:        uuid.NewString(),
		Type:      websocket.EventNotification,
		Source:    "api-service",
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Data: map[string]interface{}{
			"type":      req.Type,
			"message":   req.Message,
			"data":      req.Data,
			"sender_id": senderID,
		},
		Timestamp: time.Now(),
	}

	// PublishCriticalEvent returns publish errors (PublishEvent swallows them),
	// so 200 means the broker confirmed the message.
	if err := h.publisher.PublishCriticalEvent(c.Request.Context(), event); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Notification service unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Notification sent", "event_id": event.ID})
}
