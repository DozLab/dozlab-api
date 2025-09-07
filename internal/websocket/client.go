package websocket

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

// readPump pumps messages from the WebSocket connection to the manager
func (c *Client) readPump() {
	defer func() {
		c.Manager.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		var message Message
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Handle incoming message
		c.handleMessage(message)
	}
}

// writePump pumps messages from the manager to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The manager closed the channel
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Set timestamp if not already set
			if message.Timestamp == 0 {
				message.Timestamp = time.Now().Unix()
			}

			// Send the message as JSON
			if err := c.Conn.WriteJSON(message); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming messages from the client
func (c *Client) handleMessage(message Message) {
	// Add client context to message
	message.SessionID = c.SessionID

	switch message.Type {
	case MessageTypeTerminal:
		c.handleTerminalMessage(message)
	case MessageTypeHeartbeat:
		c.handleHeartbeat(message)
	default:
		log.Printf("Unknown message type: %s from client %s", message.Type, c.ID)
	}
}

// handleTerminalMessage processes terminal-related messages
func (c *Client) handleTerminalMessage(message Message) {
	// Parse terminal message data
	var terminalData TerminalMessage
	if data, ok := message.Data.(map[string]interface{}); ok {
		jsonData, _ := json.Marshal(data)
		json.Unmarshal(jsonData, &terminalData)
	}

	if terminalData.Command != "" {
		log.Printf("Terminal command from client %s: %s", c.ID, terminalData.Command)
		
		// TODO: Execute command in lab environment
		// For now, echo the command back as output
		response := Message{
			Type:      MessageTypeTerminal,
			SessionID: c.SessionID,
			Data: TerminalMessage{
				Output: "$ " + terminalData.Command + "\nCommand executed successfully\n",
			},
			Timestamp: time.Now().Unix(),
		}

		// Send response back to the client
		select {
		case c.Send <- response:
		default:
			log.Printf("Failed to send terminal response to client %s", c.ID)
		}

		// Also broadcast to other clients in the same session
		if c.SessionID != "" {
			c.Manager.SendToSession(c.SessionID, response)
		}
	}
}

// handleHeartbeat responds to heartbeat messages
func (c *Client) handleHeartbeat(message Message) {
	response := Message{
		Type:      MessageTypeHeartbeat,
		Data:      "pong",
		Timestamp: time.Now().Unix(),
	}

	select {
	case c.Send <- response:
	default:
		log.Printf("Failed to send heartbeat response to client %s", c.ID)
	}
}

// SendMessage sends a message to this specific client
func (c *Client) SendMessage(message Message) {
	select {
	case c.Send <- message:
	default:
		// Client's send channel is full, close the connection
		c.Manager.unregister <- c
	}
}

// SendTerminalOutput sends terminal output to the client
func (c *Client) SendTerminalOutput(output string) {
	message := Message{
		Type:      MessageTypeTerminal,
		SessionID: c.SessionID,
		Data: TerminalMessage{
			Output: output,
		},
		Timestamp: time.Now().Unix(),
	}

	c.SendMessage(message)
}

// SendProgress sends progress update to the client
func (c *Client) SendProgress(labID string, progress float64, taskID, taskStatus string) {
	message := Message{
		Type: MessageTypeProgress,
		Data: ProgressMessage{
			LabID:      labID,
			UserID:     c.UserID,
			Progress:   progress,
			TaskID:     taskID,
			TaskStatus: taskStatus,
		},
		Timestamp: time.Now().Unix(),
	}

	c.SendMessage(message)
}

// SendNotification sends a notification to the client
func (c *Client) SendNotification(title, msg, notificationType string) {
	message := Message{
		Type: MessageTypeNotification,
		Data: NotificationMessage{
			Title:   title,
			Message: msg,
			Type:    notificationType,
		},
		Timestamp: time.Now().Unix(),
	}

	c.SendMessage(message)
}

// SendError sends an error message to the client
func (c *Client) SendError(errorMsg string) {
	message := Message{
		Type: MessageTypeError,
		Data: map[string]string{
			"error": errorMsg,
		},
		Timestamp: time.Now().Unix(),
	}

	c.SendMessage(message)
}