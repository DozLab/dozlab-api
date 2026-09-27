package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dozlab-backend/internal/api/handlers"
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNotificationPublisher struct {
	events []interface{}
	err    error
}

func (f *fakeNotificationPublisher) PublishCriticalEvent(ctx context.Context, event interface{}) error {
	f.events = append(f.events, event)
	return f.err
}

func sendNotification(t *testing.T, pub *fakeNotificationPublisher, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/notifications", func(c *gin.Context) {
		c.Set("user_id", "sender-1") // set by AuthMiddleware in the real router
		c.Next()
	}, handlers.NewNotificationHandler(pub).SendNotification)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestNotificationHandler_PublishesEvent(t *testing.T) {
	pub := &fakeNotificationPublisher{}
	w := sendNotification(t, pub, `{"user_id":"u-1","session_id":"s-1","type":"lab_ready","message":"Your lab is ready","data":{"lab_id":"l-1"}}`)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, pub.events, 1)
	event, ok := pub.events[0].(*websocket.Event)
	require.True(t, ok, "expected *websocket.Event, got %T", pub.events[0])

	assert.NotEmpty(t, event.ID)
	assert.Equal(t, websocket.EventNotification, event.Type)
	assert.Equal(t, "api-service", event.Source)
	assert.Equal(t, "u-1", event.UserID)
	assert.Equal(t, "s-1", event.SessionID)
	assert.Equal(t, "lab_ready", event.Data["type"])
	assert.Equal(t, "Your lab is ready", event.Data["message"])
	assert.Equal(t, map[string]interface{}{"lab_id": "l-1"}, event.Data["data"])
	assert.Equal(t, "sender-1", event.Data["sender_id"])
	assert.False(t, event.Timestamp.IsZero())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Notification sent", resp["message"])
	assert.Equal(t, event.ID, resp["event_id"])
}

func TestNotificationHandler_InvalidJSON(t *testing.T) {
	pub := &fakeNotificationPublisher{}
	w := sendNotification(t, pub, `{"user_id":`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, pub.events)
}

func TestNotificationHandler_PublishFails(t *testing.T) {
	pub := &fakeNotificationPublisher{err: errors.New("broker down")}
	w := sendNotification(t, pub, `{"user_id":"u-1","type":"info","message":"hi"}`)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Len(t, pub.events, 1)
}
