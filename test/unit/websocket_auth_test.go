package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dozlab-backend/internal/middleware"
	"dozlab-backend/internal/websocket"
	"dozlab-backend/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wsTestSecret = "test-secret-key-32-characters-long"

func wsTestToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	tokens, err := auth.GenerateTokenPair(userID, "wsuser", "ws@example.com", "student", wsTestSecret)
	require.NoError(t, err)
	return tokens.AccessToken
}

// wsTestServer serves /ws like SetupRoutes does, with a running Manager.
func wsTestServer(t *testing.T) (*httptest.Server, *websocket.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	m := websocket.NewManager()
	go m.Start()
	router := gin.New()
	router.GET("/ws", middleware.WebSocketAuthMiddleware(wsTestSecret), m.HandleWebSocket)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, m
}

func TestWebSocketAuth_Rejects(t *testing.T) {
	srv, _ := wsTestServer(t)
	valid := wsTestToken(t, uuid.New())

	tests := []struct {
		name   string
		header http.Header
	}{
		{"no credentials", http.Header{}},
		{"invalid bearer token", http.Header{"Authorization": {"Bearer not-a-jwt"}}},
		{"non-bearer authorization", http.Header{"Authorization": {"Basic " + valid}}},
		{"sentinel without token", http.Header{"Sec-Websocket-Protocol": {middleware.WebSocketSubprotocol}}},
		{"token without sentinel", http.Header{"Sec-Websocket-Protocol": {valid}}},
		{"invalid subprotocol token", http.Header{"Sec-Websocket-Protocol": {middleware.WebSocketSubprotocol + ", not-a-jwt"}}},
		{"token signed with another secret", http.Header{"Authorization": {"Bearer " + func() string {
			tokens, _ := auth.GenerateTokenPair(uuid.New(), "x", "x@example.com", "student", "another-secret-another-secret-32")
			return tokens.AccessToken
		}()}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, resp, err := gorilla.DefaultDialer.Dial(wsURL(srv), tt.header)
			if conn != nil {
				conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestWebSocketAuth_SubprotocolReceivesNotification(t *testing.T) {
	srv, m := wsTestServer(t)
	userID := uuid.New()
	token := wsTestToken(t, userID)

	dialer := gorilla.Dialer{Subprotocols: []string{middleware.WebSocketSubprotocol, token}}
	conn, resp, err := dialer.Dial(wsURL(srv), nil)
	require.NoError(t, err)
	defer conn.Close()

	// The server must echo the sentinel, never the token
	assert.Equal(t, middleware.WebSocketSubprotocol, conn.Subprotocol())
	assert.NotContains(t, strings.Join(resp.Header.Values("Sec-Websocket-Protocol"), ","), token)

	var welcome websocket.Message
	require.NoError(t, conn.ReadJSON(&welcome))
	assert.Equal(t, websocket.MessageTypeNotification, welcome.Type)

	require.NoError(t, m.HandleNotificationEvent(context.Background(), &websocket.Event{
		ID: "n-1", Type: websocket.EventNotification, UserID: userID.String(), SessionID: "s-1",
		Data: map[string]interface{}{"type": "lab_ready", "message": "Your lab is ready"},
	}))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var got struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		Data      struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"data"`
	}
	require.NoError(t, conn.ReadJSON(&got))
	assert.Equal(t, websocket.MessageTypeNotification, got.Type)
	assert.Equal(t, "s-1", got.SessionID)
	assert.Equal(t, "n-1", got.Data.ID)
	assert.Equal(t, "lab_ready", got.Data.Type)
	assert.Equal(t, "Your lab is ready", got.Data.Message)
}

func TestWebSocketAuth_AuthorizationHeader(t *testing.T) {
	srv, _ := wsTestServer(t)
	header := http.Header{"Authorization": {"Bearer " + wsTestToken(t, uuid.New())}}

	conn, _, err := gorilla.DefaultDialer.Dial(wsURL(srv), header)
	require.NoError(t, err)
	defer conn.Close()
	assert.Empty(t, conn.Subprotocol())

	var welcome websocket.Message
	require.NoError(t, conn.ReadJSON(&welcome))
	assert.Equal(t, websocket.MessageTypeNotification, welcome.Type)
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}
