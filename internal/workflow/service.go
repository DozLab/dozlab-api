package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"dozlab-backend/internal/websocket"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WorkflowService manages workflow operations and integrates with the lab system
type WorkflowService struct {
	db            *database.Database
	orchestrator  *WorkflowOrchestrator
	wsService     *websocket.SessionService
	dagManager    *DAGManager
	progressHistory map[string]*ProgressHistory
	progressMutex   sync.RWMutex
	cleanupTicker   *time.Ticker
	stopCleanup     chan struct{}
}

// NewWorkflowService creates a new workflow service
func NewWorkflowService(db *database.Database, wsService *websocket.SessionService) *WorkflowService {
	ws := &WorkflowService{
		db:              db,
		orchestrator:    NewWorkflowOrchestrator(5), // Max 5 concurrent tasks
		wsService:       wsService,
		dagManager:      NewDAGManager(),
		progressHistory: make(map[string]*ProgressHistory),
		cleanupTicker:   time.NewTicker(10 * time.Minute), // Cleanup every 10 minutes
		stopCleanup:     make(chan struct{}),
	}
	
	// Start cleanup goroutine
	go ws.cleanupProgressHistory()
	
	return ws
}

// CreateWorkflowRequest represents a request to create a new workflow
type CreateWorkflowRequest struct {
	LabID       string          `json:"lab_id" binding:"required"`
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Tasks       []TaskDefinition `json:"tasks" binding:"required"`
}

// TaskDefinition represents a task definition in the request
type TaskDefinition struct {
	ID            string            `json:"id" binding:"required"`
	Name          string            `json:"name" binding:"required"`
	Description   string            `json:"description"`
	Type          TaskType          `json:"type" binding:"required"`
	Configuration json.RawMessage   `json:"configuration" binding:"required"`
	Dependencies  []string          `json:"dependencies"`
	Timeout       int32             `json:"timeout"`
	MaxRetries    int32             `json:"max_retries"`
}

// ExecuteWorkflowRequest represents a request to execute a workflow
type ExecuteWorkflowRequest struct {
	WorkflowID string `json:"workflow_id" binding:"required"`
	SessionID  string `json:"session_id" binding:"required"`
	UserID     string `json:"user_id" binding:"required"`
}

// WorkflowExecutionResponse represents a workflow execution response
type WorkflowExecutionResponse struct {
	ID           string                   `json:"id"`
	WorkflowID   string                   `json:"workflow_id"`
	SessionID    string                   `json:"session_id"`
	UserID       string                   `json:"user_id"`
	Status       WorkflowStatus           `json:"status"`
	Progress     *WorkflowProgress        `json:"progress,omitempty"`
	DAGAnalysis  *DAGAnalysis            `json:"dag_analysis,omitempty"`
	StartedAt    *time.Time               `json:"started_at,omitempty"`
	CompletedAt  *time.Time               `json:"completed_at,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
}

// CreateWorkflow creates a new workflow
func (ws *WorkflowService) CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*Workflow, error) {
	log.Printf("Creating workflow %s for lab %s", req.Name, req.LabID)

	// Validate lab exists
	var lab models.Lab
	if err := ws.db.DB.Where("id = ?", req.LabID).First(&lab).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("lab not found")
		}
		return nil, fmt.Errorf("failed to validate lab: %w", err)
	}

	// Create workflow
	workflow := &Workflow{
		ID:          uuid.New().String(),
		LabID:       req.LabID,
		Name:        req.Name,
		Description: req.Description,
		Version:     1,
		Status:      WorkflowStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Create tasks
	tasks := make([]Task, 0, len(req.Tasks))
	for _, taskDef := range req.Tasks {
		task := Task{
			ID:            taskDef.ID,
			WorkflowID:    workflow.ID,
			Name:          taskDef.Name,
			Description:   taskDef.Description,
			Type:          taskDef.Type,
			Configuration: taskDef.Configuration,
			Dependencies:  taskDef.Dependencies,
			Status:        TaskStatusPending,
			Timeout:       taskDef.Timeout,
			MaxRetries:    taskDef.MaxRetries,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		// Set defaults
		if task.Timeout == 0 {
			task.Timeout = 300 // 5 minutes default
		}
		if task.MaxRetries == 0 {
			task.MaxRetries = 3
		}

		tasks = append(tasks, task)
	}

	workflow.Tasks = tasks

	// Validate workflow DAG
	analysis, _, err := ws.dagManager.BuildDAG(workflow)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow DAG: %w", err)
	}

	if !analysis.IsValid {
		return nil, fmt.Errorf("workflow contains cycles: %v", analysis.Cycles)
	}

	// Save to database (Note: This would require adding workflow tables to the database)
	// For now, we'll return the workflow object
	log.Printf("Successfully created workflow %s with %d tasks", workflow.ID, len(tasks))
	return workflow, nil
}

// ExecuteWorkflow starts executing a workflow
func (ws *WorkflowService) ExecuteWorkflow(ctx context.Context, req *ExecuteWorkflowRequest) (*WorkflowExecutionResponse, error) {
	log.Printf("Starting execution of workflow %s for user %s in session %s", req.WorkflowID, req.UserID, req.SessionID)

	// For this example, we'll create a sample workflow
	// In production, this would load the workflow from the database
	workflow := ws.createSampleWorkflow(req.WorkflowID)

	// Validate workflow DAG
	analysis, _, err := ws.dagManager.BuildDAG(workflow)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow DAG: %w", err)
	}

	// Create workflow execution
	workflowExecution := &WorkflowExecution{
		ID:         uuid.New().String(),
		WorkflowID: req.WorkflowID,
		SessionID:  req.SessionID,
		UserID:     req.UserID,
		Status:     WorkflowStatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Initialize progress history with thread safety
	ws.progressMutex.Lock()
	ws.progressHistory[workflowExecution.ID] = NewProgressHistory(100)
	ws.progressMutex.Unlock()

	// Start workflow execution in background
	go func() {
		if err := ws.orchestrator.ExecuteWorkflow(ctx, workflowExecution, workflow); err != nil {
			log.Printf("Workflow execution failed: %v", err)
			
			// Notify via WebSocket
			if ws.wsService != nil {
				ws.wsService.SendSessionNotification(req.SessionID, 
					"Workflow Failed", 
					fmt.Sprintf("Workflow execution failed: %s", err.Error()), 
					"error")
			}
		} else {
			log.Printf("Workflow execution completed successfully")
			
			// Notify via WebSocket
			if ws.wsService != nil {
				ws.wsService.SendSessionNotification(req.SessionID, 
					"Workflow Completed", 
					"Workflow execution completed successfully", 
					"success")
			}
		}
	}()

	// Return execution response
	response := &WorkflowExecutionResponse{
		ID:          workflowExecution.ID,
		WorkflowID:  workflowExecution.WorkflowID,
		SessionID:   workflowExecution.SessionID,
		UserID:      workflowExecution.UserID,
		Status:      workflowExecution.Status,
		DAGAnalysis: analysis,
		CreatedAt:   workflowExecution.CreatedAt,
	}

	return response, nil
}

// GetWorkflowExecution gets the status of a workflow execution
func (ws *WorkflowService) GetWorkflowExecution(ctx context.Context, executionID string) (*WorkflowExecutionResponse, error) {
	// For this example, we'll create a sample response
	// In production, this would load from database
	
	response := &WorkflowExecutionResponse{
		ID:         executionID,
		WorkflowID: "sample-workflow",
		SessionID:  "sample-session",
		UserID:     "sample-user",
		Status:     WorkflowStatusRunning,
		CreatedAt:  time.Now().Add(-10 * time.Minute),
	}

	// Add progress information if available with thread safety
	ws.progressMutex.RLock()
	history, exists := ws.progressHistory[executionID]
	ws.progressMutex.RUnlock()
	
	if exists {
		if latest := history.GetLatestSnapshot(); latest != nil {
			response.Progress = &WorkflowProgress{
				WorkflowExecutionID: executionID,
				OverallProgress:     latest.OverallProgress,
				TaskProgress:        latest.TaskProgresses,
				EstimatedCompletion: latest.EstimatedCompletion,
				LastUpdated:         latest.Timestamp,
			}
		}
	}

	return response, nil
}

// GetWorkflowProgress gets detailed progress information
func (ws *WorkflowService) GetWorkflowProgress(ctx context.Context, executionID string) (*ProgressSnapshot, error) {
	ws.progressMutex.RLock()
	history, exists := ws.progressHistory[executionID]
	ws.progressMutex.RUnlock()
	
	if exists {
		return history.GetLatestSnapshot(), nil
	}

	return nil, fmt.Errorf("workflow execution not found")
}

// ListWorkflowExecutions lists workflow executions for a user
func (ws *WorkflowService) ListWorkflowExecutions(ctx context.Context, userID string, sessionID string) ([]*WorkflowExecutionResponse, error) {
	// For this example, return sample data
	// In production, this would query the database
	
	executions := []*WorkflowExecutionResponse{
		{
			ID:         uuid.New().String(),
			WorkflowID: "sample-workflow-1",
			SessionID:  sessionID,
			UserID:     userID,
			Status:     WorkflowStatusCompleted,
			CreatedAt:  time.Now().Add(-1 * time.Hour),
		},
		{
			ID:         uuid.New().String(),
			WorkflowID: "sample-workflow-2",
			SessionID:  sessionID,
			UserID:     userID,
			Status:     WorkflowStatusRunning,
			CreatedAt:  time.Now().Add(-30 * time.Minute),
		},
	}

	return executions, nil
}

// ValidateWorkflow validates a workflow definition
func (ws *WorkflowService) ValidateWorkflow(ctx context.Context, workflow *Workflow) (*DAGAnalysis, error) {
	analysis, _, err := ws.dagManager.BuildDAG(workflow)
	return analysis, err
}

// Helper methods

// createSampleWorkflow creates a sample workflow for demonstration
func (ws *WorkflowService) createSampleWorkflow(workflowID string) *Workflow {
	return &Workflow{
		ID:          workflowID,
		LabID:       "sample-lab",
		Name:        "Sample Workflow",
		Description: "A sample workflow for demonstration",
		Version:     1,
		Status:      WorkflowStatusPending,
		Tasks: []Task{
			{
				ID:           "task-1",
				WorkflowID:   workflowID,
				Name:         "Setup Environment",
				Description:  "Initialize the lab environment",
				Type:         TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "echo 'Setting up environment'", "timeout": 30}`),
				Dependencies: []string{},
				Status:       TaskStatusPending,
				Timeout:      30,
				MaxRetries:   2,
			},
			{
				ID:           "task-2",
				WorkflowID:   workflowID,
				Name:         "Install Dependencies",
				Description:  "Install required packages",
				Type:         TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "echo 'Installing dependencies'", "timeout": 120}`),
				Dependencies: []string{"task-1"},
				Status:       TaskStatusPending,
				Timeout:      120,
				MaxRetries:   3,
			},
			{
				ID:           "task-3",
				WorkflowID:   workflowID,
				Name:         "Run Tests",
				Description:  "Execute test suite",
				Type:         TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "echo 'Running tests'", "timeout": 180}`),
				Dependencies: []string{"task-2"},
				Status:       TaskStatusPending,
				Timeout:      180,
				MaxRetries:   2,
			},
			{
				ID:           "task-4",
				WorkflowID:   workflowID,
				Name:         "Validate Results",
				Description:  "Validate test results",
				Type:         TaskTypeValidation,
				Configuration: json.RawMessage(`{"type": "command_output", "target": "echo 'test passed'", "expected": "test passed"}`),
				Dependencies: []string{"task-3"},
				Status:       TaskStatusPending,
				Timeout:      60,
				MaxRetries:   1,
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// UpdateWorkflowProgress updates progress and notifies WebSocket clients
func (ws *WorkflowService) UpdateWorkflowProgress(executionID, sessionID string, progress *WorkflowProgress) {
	// Store progress in history with thread safety
	ws.progressMutex.RLock()
	history, exists := ws.progressHistory[executionID]
	ws.progressMutex.RUnlock()
	
	if exists {
		snapshot := ProgressSnapshot{
			WorkflowExecutionID: executionID,
			Timestamp:           time.Now(),
			OverallProgress:     progress.OverallProgress,
			TaskProgresses:      progress.TaskProgress,
			EstimatedCompletion: progress.EstimatedCompletion,
		}
		history.AddSnapshot(snapshot)
	}

	// Notify WebSocket clients
	if ws.wsService != nil {
		// Send progress update to session
		ws.wsService.UpdateSessionProgress("", "", sessionID, progress.OverallProgress, "", "")
		
		// Send detailed workflow progress notification
		notificationMsg := fmt.Sprintf("Workflow progress: %.1f%% complete (%d/%d tasks)", 
			progress.OverallProgress, progress.CompletedTasks, progress.TotalTasks)
		
		ws.wsService.SendSessionNotification(sessionID, "Workflow Progress", notificationMsg, "info")
	}
}

// StopWorkflowExecution stops a running workflow execution
func (ws *WorkflowService) StopWorkflowExecution(ctx context.Context, executionID, userID string) error {
	log.Printf("Stopping workflow execution %s for user %s", executionID, userID)
	
	// In production, this would:
	// 1. Mark the execution as stopped in database
	// 2. Cancel the execution context
	// 3. Cleanup running tasks
	// 4. Notify WebSocket clients
	
	return nil
}

// GetWorkflowTemplate creates workflow templates for common lab patterns
func (ws *WorkflowService) GetWorkflowTemplate(templateType string) (*Workflow, error) {
	switch templateType {
	case "coding-exercise":
		return ws.createCodingExerciseTemplate(), nil
	case "deployment-lab":
		return ws.createDeploymentLabTemplate(), nil
	case "security-assessment":
		return ws.createSecurityAssessmentTemplate(), nil
	default:
		return nil, fmt.Errorf("unknown workflow template: %s", templateType)
	}
}

// createCodingExerciseTemplate creates a template for coding exercises
func (ws *WorkflowService) createCodingExerciseTemplate() *Workflow {
	return &Workflow{
		ID:          uuid.New().String(),
		Name:        "Coding Exercise Template",
		Description: "Template for coding exercise workflows",
		Version:     1,
		Tasks: []Task{
			{
				ID:            "setup-env",
				Name:          "Setup Development Environment",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "mkdir -p /workspace && cd /workspace"}`),
				Dependencies:  []string{},
				Timeout:       60,
			},
			{
				ID:            "clone-repo",
				Name:          "Clone Repository",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "git clone <REPO_URL> .", "working_dir": "/workspace"}`),
				Dependencies:  []string{"setup-env"},
				Timeout:       120,
			},
			{
				ID:            "install-deps",
				Name:          "Install Dependencies",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "npm install", "working_dir": "/workspace"}`),
				Dependencies:  []string{"clone-repo"},
				Timeout:       300,
			},
			{
				ID:            "run-tests",
				Name:          "Run Tests",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "npm test", "working_dir": "/workspace"}`),
				Dependencies:  []string{"install-deps"},
				Timeout:       180,
			},
			{
				ID:            "validate-solution",
				Name:          "Validate Solution",
				Type:          TaskTypeValidation,
				Configuration: json.RawMessage(`{"type": "command_output", "target": "npm test", "expected": "All tests passed"}`),
				Dependencies:  []string{"run-tests"},
				Timeout:       60,
			},
		},
	}
}

// createDeploymentLabTemplate creates a template for deployment labs
func (ws *WorkflowService) createDeploymentLabTemplate() *Workflow {
	return &Workflow{
		ID:          uuid.New().String(),
		Name:        "Deployment Lab Template",
		Description: "Template for application deployment workflows",
		Version:     1,
		Tasks: []Task{
			{
				ID:            "build-app",
				Name:          "Build Application",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "docker build -t myapp ."}`),
				Dependencies:  []string{},
				Timeout:       300,
			},
			{
				ID:            "deploy-app",
				Name:          "Deploy Application",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "kubectl apply -f deployment.yaml"}`),
				Dependencies:  []string{"build-app"},
				Timeout:       180,
			},
			{
				ID:            "health-check",
				Name:          "Health Check",
				Type:          TaskTypeHTTP,
				Configuration: json.RawMessage(`{"method": "GET", "url": "http://localhost:8080/health", "timeout": 30}`),
				Dependencies:  []string{"deploy-app"},
				Timeout:       60,
			},
		},
	}
}

// createSecurityAssessmentTemplate creates a template for security assessments
func (ws *WorkflowService) createSecurityAssessmentTemplate() *Workflow {
	return &Workflow{
		ID:          uuid.New().String(),
		Name:        "Security Assessment Template",
		Description: "Template for security assessment workflows",
		Version:     1,
		Tasks: []Task{
			{
				ID:            "scan-dependencies",
				Name:          "Scan Dependencies",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "npm audit --json"}`),
				Dependencies:  []string{},
				Timeout:       120,
			},
			{
				ID:            "static-analysis",
				Name:          "Static Code Analysis",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "eslint . --format json"}`),
				Dependencies:  []string{},
				Timeout:       180,
			},
			{
				ID:            "security-tests",
				Name:          "Security Tests",
				Type:          TaskTypeCommand,
				Configuration: json.RawMessage(`{"command": "npm run test:security"}`),
				Dependencies:  []string{"scan-dependencies", "static-analysis"},
				Timeout:       300,
			},
			{
				ID:            "generate-report",
				Name:          "Generate Security Report",
				Type:          TaskTypeFile,
				Configuration: json.RawMessage(`{"operation": "create", "path": "/reports/security-report.json"}`),
				Dependencies:  []string{"security-tests"},
				Timeout:       60,
			},
		},
	}
}

// cleanupProgressHistory periodically removes old progress history entries
func (ws *WorkflowService) cleanupProgressHistory() {
	for {
		select {
		case <-ws.cleanupTicker.C:
			ws.performCleanup()
		case <-ws.stopCleanup:
			ws.cleanupTicker.Stop()
			return
		}
	}
}

// performCleanup removes progress history entries older than 24 hours
func (ws *WorkflowService) performCleanup() {
	ws.progressMutex.Lock()
	defer ws.progressMutex.Unlock()

	cutoffTime := time.Now().Add(-24 * time.Hour)
	toDelete := make([]string, 0)

	for executionID, history := range ws.progressHistory {
		if latest := history.GetLatestSnapshot(); latest != nil {
			if latest.Timestamp.Before(cutoffTime) {
				toDelete = append(toDelete, executionID)
			}
		}
	}

	// Remove expired entries
	for _, executionID := range toDelete {
		delete(ws.progressHistory, executionID)
		log.Printf("Cleaned up progress history for workflow execution: %s", executionID)
	}

	if len(toDelete) > 0 {
		log.Printf("Cleaned up %d expired progress history entries", len(toDelete))
	}
}

// Shutdown gracefully shuts down the workflow service
func (ws *WorkflowService) Shutdown() {
	log.Println("Shutting down workflow service...")
	close(ws.stopCleanup)
}