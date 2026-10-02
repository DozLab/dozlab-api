package services

import (
	"context"
	"encoding/json"
	"fmt"

	"dozlab-backend/internal/clients"
)

// ServiceClients manages communication with other microservices
type ServiceClients struct {
	websocketClient    *clients.HTTPClient
	examinerClient     *clients.HTTPClient
	workflowClient     *clients.HTTPClient
}

// NewServiceClients creates service clients based on environment configuration
func NewServiceClients(config ServiceConfig) *ServiceClients {
	return &ServiceClients{
		websocketClient:  clients.NewHTTPClient(config.WebSocketServiceURL),
		examinerClient:   clients.NewHTTPClient(config.ExaminerServiceURL),
		workflowClient:   clients.NewHTTPClient(config.WorkflowServiceURL),
	}
}

// ServiceConfig holds configuration for service communication
type ServiceConfig struct {
	WebSocketServiceURL   string
	ExaminerServiceURL    string
	WorkflowServiceURL    string
}

// WebSocket Service Communication

// NotificationRequest is the body SendNotification posts to the WebSocket service.
type NotificationRequest struct {
	UserID    string      `json:"user_id"`
	SessionID string      `json:"session_id,omitempty"`
	Type      string      `json:"type"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

// SendNotification sends a notification via WebSocket service
func (s *ServiceClients) SendNotification(ctx context.Context, req NotificationRequest) error {
	_, err := s.websocketClient.Post(ctx, "/notifications", req)
	return err
}

// GetWebSocketStats retrieves WebSocket connection statistics
func (s *ServiceClients) GetWebSocketStats(ctx context.Context) (map[string]interface{}, error) {
	resp, err := s.websocketClient.Get(ctx, "/stats")
	if err != nil {
		return nil, err
	}
	
	var stats map[string]interface{}
	err = json.Unmarshal(resp, &stats)
	return stats, err
}


// Examiner Service Communication

// ValidationRequest asks the Examiner service to check a lab session against Rules.
type ValidationRequest struct {
	SessionID string      `json:"session_id"`
	LabID     string      `json:"lab_id"`
	Rules     interface{} `json:"rules"`
}

// ValidationResponse is the Examiner service's result for a ValidationRequest.
type ValidationResponse struct {
	Valid   bool        `json:"valid"`
	Score   int         `json:"score"`
	Results interface{} `json:"results"`
	Error   string      `json:"error,omitempty"`
}

// ValidateLabSession validates a lab session via Examiner service
func (s *ServiceClients) ValidateLabSession(ctx context.Context, req ValidationRequest) (*ValidationResponse, error) {
	resp, err := s.examinerClient.Post(ctx, "/validate", req)
	if err != nil {
		return nil, err
	}
	
	var validation ValidationResponse
	err = json.Unmarshal(resp, &validation)
	return &validation, err
}

// Workflow Service Communication

// WorkflowRequest asks the Workflow service to run Steps for a session.
type WorkflowRequest struct {
	Name      string      `json:"name"`
	Steps     interface{} `json:"steps"`
	SessionID string      `json:"session_id"`
}

// WorkflowResponse describes a workflow run returned by the Workflow service.
type WorkflowResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Error    string `json:"error,omitempty"`
}

// ExecuteWorkflow executes a workflow via Workflow service
func (s *ServiceClients) ExecuteWorkflow(ctx context.Context, req WorkflowRequest) (*WorkflowResponse, error) {
	resp, err := s.workflowClient.Post(ctx, "/execute", req)
	if err != nil {
		return nil, err
	}
	
	var workflow WorkflowResponse
	err = json.Unmarshal(resp, &workflow)
	return &workflow, err
}

// GetWorkflowStatus gets workflow status via Workflow service
func (s *ServiceClients) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowResponse, error) {
	resp, err := s.workflowClient.Get(ctx, fmt.Sprintf("/status/%s", workflowID))
	if err != nil {
		return nil, err
	}
	
	var workflow WorkflowResponse
	err = json.Unmarshal(resp, &workflow)
	return &workflow, err
}
