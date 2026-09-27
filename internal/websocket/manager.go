package websocket

import (
	"log"
	"net/http"
	"sync"
	"time"

	"dozlab-backend/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WebSocket connection upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, implement proper CORS checking
		return true
	},
	// Browsers send ["dozlab.bearer", <JWT>]; only the first is echoed back
	Subprotocols: []string{middleware.WebSocketSubprotocol},
}

// Message types for WebSocket communication
const (
	MessageTypeTerminal       = "terminal"
	MessageTypeProgress       = "progress"
	MessageTypeNotification   = "notification"
	MessageTypeSessionStatus  = "session_status"
	MessageTypeHeartbeat      = "heartbeat"
	MessageTypeError          = "error"
)

// WebSocket message structure
type Message struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id,omitempty"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// Terminal message data
type TerminalMessage struct {
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Progress message data
type ProgressMessage struct {
	LabID      string  `json:"lab_id"`
	UserID     string  `json:"user_id"`
	Progress   float64 `json:"progress"`
	TaskID     string  `json:"task_id,omitempty"`
	TaskStatus string  `json:"task_status,omitempty"`
}

// Notification message data
type NotificationMessage struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Type    string `json:"type"` // info, success, warning, error

	// Set for notifications that came from the event bus
	ID       string      `json:"id,omitempty"`
	Data     interface{} `json:"data,omitempty"`
	SenderID string      `json:"sender_id,omitempty"`
}

// Client represents a WebSocket client connection
type Client struct {
	ID        string
	UserID    string
	SessionID string
	Conn      *websocket.Conn
	Send      chan Message
	Manager   *Manager
}

// Manager manages WebSocket connections
type Manager struct {
	clients    map[string]*Client     // client ID -> Client
	sessions   map[string][]*Client   // session ID -> []Client
	users      map[string][]*Client   // user ID -> []Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	mutex      sync.RWMutex
}

// NewManager creates a new WebSocket manager
func NewManager() *Manager {
	return &Manager{
		clients:    make(map[string]*Client),
		sessions:   make(map[string][]*Client),
		users:      make(map[string][]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message),
	}
}

// Start runs the WebSocket manager
func (m *Manager) Start() {
	for {
		select {
		case client := <-m.register:
			m.registerClient(client)

		case client := <-m.unregister:
			m.unregisterClient(client)

		case message := <-m.broadcast:
			m.broadcastMessage(message)
		}
	}
}

// RegisterClient adds a new client to the manager
func (m *Manager) registerClient(client *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.clients[client.ID] = client

	// Add to session group
	if client.SessionID != "" {
		m.sessions[client.SessionID] = append(m.sessions[client.SessionID], client)
	}

	// Add to user group
	if client.UserID != "" {
		m.users[client.UserID] = append(m.users[client.UserID], client)
	}

	log.Printf("Client %s connected (User: %s, Session: %s)", client.ID, client.UserID, client.SessionID)

	// Send welcome message
	welcome := Message{
		Type: MessageTypeNotification,
		Data: NotificationMessage{
			Title:   "Connected",
			Message: "Successfully connected to Dozlab",
			Type:    "success",
		},
		Timestamp: getCurrentTimestamp(),
	}
	
	select {
	case client.Send <- welcome:
	default:
		close(client.Send)
	}
}

// UnregisterClient removes a client from the manager
func (m *Manager) unregisterClient(client *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.clients[client.ID]; ok {
		delete(m.clients, client.ID)
		close(client.Send)

		// Remove from session group
		if client.SessionID != "" {
			if clients, exists := m.sessions[client.SessionID]; exists {
				m.sessions[client.SessionID] = removeClientFromSlice(clients, client)
				if len(m.sessions[client.SessionID]) == 0 {
					delete(m.sessions, client.SessionID)
				}
			}
		}

		// Remove from user group
		if client.UserID != "" {
			if clients, exists := m.users[client.UserID]; exists {
				m.users[client.UserID] = removeClientFromSlice(clients, client)
				if len(m.users[client.UserID]) == 0 {
					delete(m.users, client.UserID)
				}
			}
		}

		log.Printf("Client %s disconnected", client.ID)
	}
}

// BroadcastMessage sends a message to appropriate clients
func (m *Manager) broadcastMessage(message Message) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var targetClients []*Client

	// Determine target clients based on message context
	if message.SessionID != "" {
		// Send to all clients in the session
		if clients, exists := m.sessions[message.SessionID]; exists {
			targetClients = clients
		}
	} else {
		// Broadcast to all connected clients
		for _, client := range m.clients {
			targetClients = append(targetClients, client)
		}
	}

	// Send message to target clients
	for _, client := range targetClients {
		select {
		case client.Send <- message:
		default:
			// Client's send channel is blocked, unregister it
			m.unregisterLater(client)
		}
	}
}

// unregisterLater unregisters a client without blocking. Callers hold m.mutex
// (and broadcastMessage runs on the Start goroutine that reads m.unregister),
// so a direct send on m.unregister would deadlock.
func (m *Manager) unregisterLater(client *Client) {
	go func() { m.unregister <- client }()
}

// SendToUser sends a message to all connections of a specific user
func (m *Manager) SendToUser(userID string, message Message) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if clients, exists := m.users[userID]; exists {
		for _, client := range clients {
			select {
			case client.Send <- message:
			default:
				m.unregisterLater(client)
			}
		}
	}
}

// SendToSession sends a message to all clients in a session
func (m *Manager) SendToSession(sessionID string, message Message) {
	message.SessionID = sessionID
	m.broadcast <- message
}

// Broadcast sends a message to all connected clients
func (m *Manager) Broadcast(message Message) {
	m.broadcast <- message
}

// GetSessionClients returns all clients connected to a session
func (m *Manager) GetSessionClients(sessionID string) []*Client {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if clients, exists := m.sessions[sessionID]; exists {
		// Return a copy of the slice
		result := make([]*Client, len(clients))
		copy(result, clients)
		return result
	}
	return nil
}

// GetUserClients returns all clients connected for a user
func (m *Manager) GetUserClients(userID string) []*Client {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if clients, exists := m.users[userID]; exists {
		// Return a copy of the slice
		result := make([]*Client, len(clients))
		copy(result, clients)
		return result
	}
	return nil
}

// GetStats returns connection statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return map[string]interface{}{
		"total_clients":    len(m.clients),
		"active_sessions":  len(m.sessions),
		"connected_users":  len(m.users),
	}
}

// HandleWebSocket handles incoming WebSocket connections
func (m *Manager) HandleWebSocket(c *gin.Context) {
	// Extract user information from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Get session ID from query parameter (optional)
	sessionID := c.Query("session_id")

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Create new client
	client := &Client{
		ID:        uuid.New().String(),
		UserID:    userID.(uuid.UUID).String(),
		SessionID: sessionID,
		Conn:      conn,
		Send:      make(chan Message, 256),
		Manager:   m,
	}

	// Register client with manager
	m.register <- client

	// Start client goroutines
	go client.writePump()
	go client.readPump()
}

// Helper functions
func removeClientFromSlice(clients []*Client, target *Client) []*Client {
	for i, client := range clients {
		if client.ID == target.ID {
			return append(clients[:i], clients[i+1:]...)
		}
	}
	return clients
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}