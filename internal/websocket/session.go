package websocket

import (
	"log"
	"time"
)

// SessionService provides WebSocket session management functionality
type SessionService struct {
	manager *Manager
}

// NewSessionService creates a new session service
func NewSessionService(manager *Manager) *SessionService {
	return &SessionService{
		manager: manager,
	}
}

// CreateLabSession creates a new lab session and notifies connected clients
func (s *SessionService) CreateLabSession(userID, labID, sessionID string) {
	log.Printf("Creating lab session %s for user %s in lab %s", sessionID, userID, labID)
	
	// Notify user about session creation
	message := Message{
		Type:      MessageTypeSessionStatus,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"status":     "created",
			"session_id": sessionID,
			"lab_id":     labID,
			"user_id":    userID,
		},
		Timestamp: time.Now().Unix(),
	}

	s.manager.SendToUser(userID, message)
}

// StartLabSession starts a lab session and notifies connected clients
func (s *SessionService) StartLabSession(userID, labID, sessionID string) {
	log.Printf("Starting lab session %s for user %s in lab %s", sessionID, userID, labID)
	
	// Notify session participants
	message := Message{
		Type:      MessageTypeSessionStatus,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"status":     "started",
			"session_id": sessionID,
			"lab_id":     labID,
			"user_id":    userID,
		},
		Timestamp: time.Now().Unix(),
	}

	s.manager.SendToSession(sessionID, message)
}

// UpdateSessionProgress updates session progress and broadcasts to participants
func (s *SessionService) UpdateSessionProgress(userID, labID, sessionID string, progress float64, taskID, taskStatus string) {
	log.Printf("Updating progress for session %s: %.1f%% (task: %s, status: %s)", sessionID, progress, taskID, taskStatus)
	
	// Broadcast progress to all session participants
	clients := s.manager.GetSessionClients(sessionID)
	for _, client := range clients {
		client.SendProgress(labID, progress, taskID, taskStatus)
	}

	// Also send to user's other connections (different tabs/devices)
	s.manager.SendToUser(userID, Message{
		Type:      MessageTypeProgress,
		SessionID: sessionID,
		Data: ProgressMessage{
			LabID:      labID,
			UserID:     userID,
			Progress:   progress,
			TaskID:     taskID,
			TaskStatus: taskStatus,
		},
		Timestamp: time.Now().Unix(),
	})
}

// CompleteLabSession completes a lab session and notifies participants
func (s *SessionService) CompleteLabSession(userID, labID, sessionID string, finalScore float64) {
	log.Printf("Completing lab session %s for user %s with score %.1f", sessionID, userID, finalScore)
	
	// Send completion notification
	message := Message{
		Type:      MessageTypeSessionStatus,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"status":      "completed",
			"session_id":  sessionID,
			"lab_id":      labID,
			"user_id":     userID,
			"final_score": finalScore,
			"completed_at": time.Now().Unix(),
		},
		Timestamp: time.Now().Unix(),
	}

	s.manager.SendToSession(sessionID, message)
	
	// Send congratulations notification
	s.SendSessionNotification(sessionID, "Lab Completed!", 
		"Congratulations! You have successfully completed the lab.", "success")
}

// TerminateLabSession terminates a lab session (cleanup, error, timeout)
func (s *SessionService) TerminateLabSession(userID, labID, sessionID, reason string) {
	log.Printf("Terminating lab session %s for user %s. Reason: %s", sessionID, userID, reason)
	
	message := Message{
		Type:      MessageTypeSessionStatus,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"status":     "terminated",
			"session_id": sessionID,
			"lab_id":     labID,
			"user_id":    userID,
			"reason":     reason,
		},
		Timestamp: time.Now().Unix(),
	}

	s.manager.SendToSession(sessionID, message)
	
	// Send termination notification
	s.SendSessionNotification(sessionID, "Session Terminated", 
		"Your lab session has been terminated: " + reason, "warning")
}

// CreateSession prepares a session for WebSocket clients. Session groups are
// created lazily when the first client registers, so this only logs.
func (s *SessionService) CreateSession(sessionID string) {
	log.Printf("WebSocket session %s ready", sessionID)
}

// CloseSession notifies all clients in a session that it has ended
func (s *SessionService) CloseSession(sessionID, reason string) {
	s.manager.SendToSession(sessionID, Message{
		Type:      MessageTypeSessionStatus,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"status":     "terminated",
			"session_id": sessionID,
			"reason":     reason,
		},
		Timestamp: time.Now().Unix(),
	})
}

// BroadcastToSession sends an event payload to all clients in a session.
// The payload's "type" field, if set, becomes the message type.
func (s *SessionService) BroadcastToSession(sessionID string, data map[string]interface{}) error {
	msgType := MessageTypeNotification
	if t, ok := data["type"].(string); ok && t != "" {
		msgType = t
	}

	s.manager.SendToSession(sessionID, Message{
		Type:      msgType,
		SessionID: sessionID,
		Data:      data,
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// SendTerminalOutput sends terminal output to all clients in a session
func (s *SessionService) SendTerminalOutput(sessionID, output string) {
	clients := s.manager.GetSessionClients(sessionID)
	for _, client := range clients {
		client.SendTerminalOutput(output)
	}
}

// SendSessionNotification sends a notification to all clients in a session
func (s *SessionService) SendSessionNotification(sessionID, title, message, notificationType string) {
	clients := s.manager.GetSessionClients(sessionID)
	for _, client := range clients {
		client.SendNotification(title, message, notificationType)
	}
}

// SendSessionError sends an error message to all clients in a session
func (s *SessionService) SendSessionError(sessionID, errorMsg string) {
	clients := s.manager.GetSessionClients(sessionID)
	for _, client := range clients {
		client.SendError(errorMsg)
	}
}

// GetSessionStats returns statistics for a specific session
func (s *SessionService) GetSessionStats(sessionID string) map[string]interface{} {
	clients := s.manager.GetSessionClients(sessionID)
	
	userIDs := make([]string, 0, len(clients))
	for _, client := range clients {
		userIDs = append(userIDs, client.UserID)
	}
	
	return map[string]interface{}{
		"session_id":      sessionID,
		"connected_users": len(clients),
		"user_ids":        userIDs,
		"last_updated":    time.Now().Unix(),
	}
}