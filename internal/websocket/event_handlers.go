package websocket

import (
	"context"
	"fmt"
	"log"

	"dozlab-backend/internal/database"
)

// EventHandlers provides common event handlers for the system
type EventHandlers struct {
	db        *database.Database
	wsService *SessionService
	eventBus  *RedisEventBus
}

// NewEventHandlers creates a new event handlers instance
func NewEventHandlers(db *database.Database, wsService *SessionService, eventBus *RedisEventBus) *EventHandlers {
	return &EventHandlers{
		db:        db,
		wsService: wsService,
		eventBus:  eventBus,
	}
}

// RegisterAllHandlers registers all event handlers
func (h *EventHandlers) RegisterAllHandlers() {
	// Lab event handlers
	h.eventBus.RegisterHandler(EventLabStarted, h.HandleLabStarted)
	h.eventBus.RegisterHandler(EventLabStopped, h.HandleLabStopped)
	h.eventBus.RegisterHandler(EventLabCompleted, h.HandleLabCompleted)
	h.eventBus.RegisterHandler(EventLabFailed, h.HandleLabFailed)

	// Session event handlers
	h.eventBus.RegisterHandler(EventSessionCreated, h.HandleSessionCreated)
	h.eventBus.RegisterHandler(EventSessionJoined, h.HandleSessionJoined)
	h.eventBus.RegisterHandler(EventSessionLeft, h.HandleSessionLeft)
	h.eventBus.RegisterHandler(EventSessionTerminated, h.HandleSessionTerminated)

	// Workflow event handlers
	h.eventBus.RegisterHandler(EventWorkflowStarted, h.HandleWorkflowStarted)
	h.eventBus.RegisterHandler(EventWorkflowCompleted, h.HandleWorkflowCompleted)
	h.eventBus.RegisterHandler(EventWorkflowFailed, h.HandleWorkflowFailed)
	h.eventBus.RegisterHandler(EventTaskStarted, h.HandleTaskStarted)
	h.eventBus.RegisterHandler(EventTaskCompleted, h.HandleTaskCompleted)
	h.eventBus.RegisterHandler(EventTaskFailed, h.HandleTaskFailed)

	// File system event handlers
	h.eventBus.RegisterHandler(EventFileCreated, h.HandleFileCreated)
	h.eventBus.RegisterHandler(EventFileModified, h.HandleFileModified)
	h.eventBus.RegisterHandler(EventFileDeleted, h.HandleFileDeleted)
	h.eventBus.RegisterHandler(EventFileMoved, h.HandleFileMoved)
	h.eventBus.RegisterHandler(EventFileShared, h.HandleFileShared)

	// Terminal event handlers
	h.eventBus.RegisterHandler(EventTerminalStarted, h.HandleTerminalStarted)
	h.eventBus.RegisterHandler(EventTerminalOutput, h.HandleTerminalOutput)
	h.eventBus.RegisterHandler(EventTerminalClosed, h.HandleTerminalClosed)

	// Resource event handlers
	h.eventBus.RegisterHandler(EventResourceCreated, h.HandleResourceCreated)
	h.eventBus.RegisterHandler(EventResourceUpdated, h.HandleResourceUpdated)
	h.eventBus.RegisterHandler(EventResourceDeleted, h.HandleResourceDeleted)
}

// Lab Event Handlers

func (h *EventHandlers) HandleLabStarted(ctx context.Context, event *Event) error {
	log.Printf("Lab started: %s", event.LabID)

	// Update lab status in database
	if err := h.updateLabStatus(ctx, event.LabID, "running"); err != nil {
		return fmt.Errorf("failed to update lab status: %w", err)
	}

	// Notify WebSocket clients
	message := map[string]interface{}{
		"type":    "lab.status",
		"lab_id":  event.LabID,
		"status":  "running",
		"message": "Lab has started successfully",
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleLabStopped(ctx context.Context, event *Event) error {
	log.Printf("Lab stopped: %s", event.LabID)

	// Update lab status in database
	if err := h.updateLabStatus(ctx, event.LabID, "stopped"); err != nil {
		return fmt.Errorf("failed to update lab status: %w", err)
	}

	// Notify WebSocket clients
	message := map[string]interface{}{
		"type":    "lab.status",
		"lab_id":  event.LabID,
		"status":  "stopped",
		"message": "Lab has been stopped",
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleLabCompleted(ctx context.Context, event *Event) error {
	log.Printf("Lab completed: %s", event.LabID)

	// Update lab status and completion time
	if err := h.updateLabStatus(ctx, event.LabID, "completed"); err != nil {
		return fmt.Errorf("failed to update lab status: %w", err)
	}

	// Create completion record
	if err := h.createLabCompletion(ctx, event); err != nil {
		return fmt.Errorf("failed to create completion record: %w", err)
	}

	// Notify WebSocket clients
	message := map[string]interface{}{
		"type":    "lab.status",
		"lab_id":  event.LabID,
		"status":  "completed",
		"message": "Lab completed successfully!",
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleLabFailed(ctx context.Context, event *Event) error {
	log.Printf("Lab failed: %s", event.LabID)

	// Update lab status
	if err := h.updateLabStatus(ctx, event.LabID, "failed"); err != nil {
		return fmt.Errorf("failed to update lab status: %w", err)
	}

	// Log failure details
	if err := h.logLabFailure(ctx, event); err != nil {
		return fmt.Errorf("failed to log failure: %w", err)
	}

	// Notify WebSocket clients
	message := map[string]interface{}{
		"type":    "lab.status",
		"lab_id":  event.LabID,
		"status":  "failed",
		"message": "Lab execution failed",
		"error":   event.Data["error"],
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

// Session Event Handlers

func (h *EventHandlers) HandleSessionCreated(ctx context.Context, event *Event) error {
	log.Printf("Session created: %s", event.SessionID)

	// Create session event stream
	if err := h.eventBus.CreateSessionEventStream(ctx, event.SessionID); err != nil {
		return fmt.Errorf("failed to create session event stream: %w", err)
	}

	// Initialize session in WebSocket service
	if h.wsService != nil {
		h.wsService.CreateSession(event.SessionID)
	}

	return nil
}

func (h *EventHandlers) HandleSessionJoined(ctx context.Context, event *Event) error {
	log.Printf("User %s joined session %s", event.UserID, event.SessionID)

	// Add event to session stream
	if err := h.eventBus.AddSessionEvent(ctx, event.SessionID, event); err != nil {
		return fmt.Errorf("failed to add session event: %w", err)
	}

	// Notify other session participants
	message := map[string]interface{}{
		"type":       "user.joined",
		"user_id":    event.UserID,
		"session_id": event.SessionID,
		"message":    "User joined the session",
		"data":       event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleSessionLeft(ctx context.Context, event *Event) error {
	log.Printf("User %s left session %s", event.UserID, event.SessionID)

	// Add event to session stream
	if err := h.eventBus.AddSessionEvent(ctx, event.SessionID, event); err != nil {
		return fmt.Errorf("failed to add session event: %w", err)
	}

	// Notify other session participants
	message := map[string]interface{}{
		"type":       "user.left",
		"user_id":    event.UserID,
		"session_id": event.SessionID,
		"message":    "User left the session",
		"data":       event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleSessionTerminated(ctx context.Context, event *Event) error {
	log.Printf("Session terminated: %s", event.SessionID)

	// Clean up session resources
	if h.wsService != nil {
		h.wsService.CloseSession(event.SessionID, "Session terminated")
	}

	// Archive session data (if needed)
	return h.archiveSessionData(ctx, event.SessionID)
}

// Workflow Event Handlers

func (h *EventHandlers) HandleWorkflowStarted(ctx context.Context, event *Event) error {
	log.Printf("Workflow started: %s", event.Data["workflow_id"])

	message := map[string]interface{}{
		"type":        "workflow.status",
		"workflow_id": event.Data["workflow_id"],
		"status":      "running",
		"message":     "Workflow execution started",
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleWorkflowCompleted(ctx context.Context, event *Event) error {
	log.Printf("Workflow completed: %s", event.Data["workflow_id"])

	message := map[string]interface{}{
		"type":        "workflow.status",
		"workflow_id": event.Data["workflow_id"],
		"status":      "completed",
		"message":     "Workflow execution completed successfully",
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleWorkflowFailed(ctx context.Context, event *Event) error {
	log.Printf("Workflow failed: %s", event.Data["workflow_id"])

	message := map[string]interface{}{
		"type":        "workflow.status",
		"workflow_id": event.Data["workflow_id"],
		"status":      "failed",
		"message":     "Workflow execution failed",
		"error":       event.Data["error"],
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleTaskStarted(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":    "task.status",
		"task_id": event.Data["task_id"],
		"status":  "running",
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleTaskCompleted(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":    "task.status",
		"task_id": event.Data["task_id"],
		"status":  "completed",
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleTaskFailed(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":    "task.status",
		"task_id": event.Data["task_id"],
		"status":  "failed",
		"error":   event.Data["error"],
		"data":    event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

// File System Event Handlers

func (h *EventHandlers) HandleFileCreated(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":     "file.created",
		"file_id":  event.Data["file_id"],
		"path":     event.Data["path"],
		"name":     event.Data["name"],
		"user_id":  event.UserID,
		"data":     event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleFileModified(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":     "file.modified",
		"file_id":  event.Data["file_id"],
		"path":     event.Data["path"],
		"user_id":  event.UserID,
		"changes":  event.Data["changes"],
		"data":     event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleFileDeleted(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":     "file.deleted",
		"file_id":  event.Data["file_id"],
		"path":     event.Data["path"],
		"user_id":  event.UserID,
		"data":     event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleFileMoved(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":      "file.moved",
		"file_id":   event.Data["file_id"],
		"old_path":  event.Data["old_path"],
		"new_path":  event.Data["new_path"],
		"user_id":   event.UserID,
		"data":      event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleFileShared(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":        "file.shared",
		"file_id":     event.Data["file_id"],
		"shared_by":   event.UserID,
		"shared_with": event.Data["shared_with"],
		"permissions": event.Data["permissions"],
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

// Terminal Event Handlers

func (h *EventHandlers) HandleTerminalStarted(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":        "terminal.started",
		"terminal_id": event.Data["terminal_id"],
		"user_id":     event.UserID,
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleTerminalOutput(ctx context.Context, event *Event) error {
	// Terminal output is typically handled separately for performance
	// This handler can be used for logging or monitoring
	return nil
}

func (h *EventHandlers) HandleTerminalClosed(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":        "terminal.closed",
		"terminal_id": event.Data["terminal_id"],
		"user_id":     event.UserID,
		"data":        event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

// Resource Event Handlers

func (h *EventHandlers) HandleResourceCreated(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":          "resource.created",
		"resource_id":   event.Data["resource_id"],
		"resource_type": event.Data["resource_type"],
		"data":          event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleResourceUpdated(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":          "resource.updated",
		"resource_id":   event.Data["resource_id"],
		"resource_type": event.Data["resource_type"],
		"changes":       event.Data["changes"],
		"data":          event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

func (h *EventHandlers) HandleResourceDeleted(ctx context.Context, event *Event) error {
	message := map[string]interface{}{
		"type":          "resource.deleted",
		"resource_id":   event.Data["resource_id"],
		"resource_type": event.Data["resource_type"],
		"data":          event.Data,
	}

	return h.broadcastToSession(event.SessionID, message)
}

// Helper methods

func (h *EventHandlers) broadcastToSession(sessionID string, message map[string]interface{}) error {
	if h.wsService == nil {
		return nil // WebSocket service not available
	}

	return h.wsService.BroadcastToSession(sessionID, message)
}

func (h *EventHandlers) updateLabStatus(ctx context.Context, labID, status string) error {
	// Implementation would update lab status in database
	// For now, return nil as database structure isn't fully defined
	log.Printf("Updating lab %s status to %s", labID, status)
	return nil
}

func (h *EventHandlers) createLabCompletion(ctx context.Context, event *Event) error {
	// Implementation would create a lab completion record
	log.Printf("Creating lab completion record for lab %s by user %s", event.LabID, event.UserID)
	return nil
}

func (h *EventHandlers) logLabFailure(ctx context.Context, event *Event) error {
	// Implementation would log failure details
	log.Printf("Logging lab failure for lab %s: %v", event.LabID, event.Data["error"])
	return nil
}

func (h *EventHandlers) archiveSessionData(ctx context.Context, sessionID string) error {
	// Implementation would archive session data
	log.Printf("Archiving session data for session %s", sessionID)
	return nil
}