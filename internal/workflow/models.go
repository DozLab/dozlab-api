package workflow

import (
	"encoding/json"
	"time"
)

// TaskStatus represents the status of a task in the workflow
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusSkipped   TaskStatus = "skipped"
)

// TaskType represents different types of tasks that can be executed
type TaskType string

const (
	TaskTypeCommand     TaskType = "command"     // Execute shell command
	TaskTypeValidation  TaskType = "validation"  // Validate output/condition
	TaskTypeFile        TaskType = "file"        // File operation (create, modify)
	TaskTypeHTTP        TaskType = "http"        // HTTP request/API call
	TaskTypeManual      TaskType = "manual"      // Manual user action
	TaskTypeConditional TaskType = "conditional" // Conditional branching
)

// Task represents a single task in the workflow DAG
type Task struct {
	ID           string            `json:"id" gorm:"primary_key"`
	WorkflowID   string            `json:"workflow_id" gorm:"index"`
	Name         string            `json:"name" gorm:"not null"`
	Description  string            `json:"description"`
	Type         TaskType          `json:"type" gorm:"not null"`
	Configuration json.RawMessage  `json:"configuration" gorm:"type:jsonb"` // Task-specific config
	Dependencies  []string          `json:"dependencies" gorm:"type:jsonb"` // Task IDs this task depends on
	Status        TaskStatus        `json:"status" gorm:"default:'pending'"`
	Result        json.RawMessage   `json:"result" gorm:"type:jsonb"`       // Task execution result
	ErrorMessage  string            `json:"error_message,omitempty"`
	StartedAt     *time.Time        `json:"started_at"`
	CompletedAt   *time.Time        `json:"completed_at"`
	Timeout       int32             `json:"timeout" gorm:"default:300"`     // Timeout in seconds
	Retries       int32             `json:"retries" gorm:"default:0"`       // Number of retries attempted
	MaxRetries    int32             `json:"max_retries" gorm:"default:3"`   // Maximum retries allowed
	CreatedAt     time.Time         `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time         `json:"updated_at" gorm:"autoUpdateTime"`
}

// Workflow represents a complete workflow DAG
type Workflow struct {
	ID          string            `json:"id" gorm:"primary_key"`
	LabID       string            `json:"lab_id" gorm:"index"`
	Name        string            `json:"name" gorm:"not null"`
	Description string            `json:"description"`
	Version     int32             `json:"version" gorm:"default:1"`
	Metadata    json.RawMessage   `json:"metadata" gorm:"type:jsonb"`
	Tasks       []Task            `json:"tasks" gorm:"foreignKey:WorkflowID"`
	Status      WorkflowStatus    `json:"status" gorm:"default:'pending'"`
	CreatedAt   time.Time         `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time         `json:"updated_at" gorm:"autoUpdateTime"`
}

// WorkflowStatus represents the overall status of a workflow
type WorkflowStatus string

const (
	WorkflowStatusPending   WorkflowStatus = "pending"
	WorkflowStatusRunning   WorkflowStatus = "running"
	WorkflowStatusCompleted WorkflowStatus = "completed"
	WorkflowStatusFailed    WorkflowStatus = "failed"
	WorkflowStatusPaused    WorkflowStatus = "paused"
)

// WorkflowExecution represents a specific execution instance of a workflow
type WorkflowExecution struct {
	ID           string                   `json:"id" gorm:"primary_key"`
	WorkflowID   string                   `json:"workflow_id" gorm:"index"`
	SessionID    string                   `json:"session_id" gorm:"index"`
	UserID       string                   `json:"user_id" gorm:"index"`
	Status       WorkflowStatus           `json:"status" gorm:"default:'pending'"`
	TaskResults  json.RawMessage          `json:"task_results" gorm:"type:jsonb"` // Map of task_id -> result
	Context      json.RawMessage          `json:"context" gorm:"type:jsonb"`      // Execution context variables
	StartedAt    *time.Time               `json:"started_at"`
	CompletedAt  *time.Time               `json:"completed_at"`
	CreatedAt    time.Time                `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time                `json:"updated_at" gorm:"autoUpdateTime"`
}

// TaskExecution represents the execution state of a task within a workflow execution
type TaskExecution struct {
	ID                string         `json:"id" gorm:"primary_key"`
	WorkflowExecutionID string       `json:"workflow_execution_id" gorm:"index"`
	TaskID            string         `json:"task_id" gorm:"index"`
	Status            TaskStatus     `json:"status" gorm:"default:'pending'"`
	Result            json.RawMessage `json:"result" gorm:"type:jsonb"`
	ErrorMessage      string         `json:"error_message,omitempty"`
	StartedAt         *time.Time     `json:"started_at"`
	CompletedAt       *time.Time     `json:"completed_at"`
	Retries           int32          `json:"retries" gorm:"default:0"`
	CreatedAt         time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
}

// Task Configuration Structs

// CommandTaskConfig represents configuration for command execution tasks
type CommandTaskConfig struct {
	Command     string            `json:"command"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Timeout     int32             `json:"timeout,omitempty"`
}

// ValidationTaskConfig represents configuration for validation tasks
type ValidationTaskConfig struct {
	Type        string          `json:"type"` // "file_exists", "command_output", "http_response"
	Target      string          `json:"target"`
	Expected    json.RawMessage `json:"expected"`
	Timeout     int32           `json:"timeout,omitempty"`
}

// FileTaskConfig represents configuration for file operation tasks
type FileTaskConfig struct {
	Operation   string `json:"operation"` // "create", "modify", "delete", "copy"
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// HTTPTaskConfig represents configuration for HTTP tasks
type HTTPTaskConfig struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Timeout int32             `json:"timeout,omitempty"`
}

// ManualTaskConfig represents configuration for manual tasks
type ManualTaskConfig struct {
	Instructions    string          `json:"instructions"`
	ValidationSteps []string        `json:"validation_steps,omitempty"`
	Resources       []string        `json:"resources,omitempty"` // URLs or file paths
}

// ConditionalTaskConfig represents configuration for conditional branching
type ConditionalTaskConfig struct {
	Condition     string   `json:"condition"`     // Expression to evaluate
	TrueBranch    []string `json:"true_branch"`   // Task IDs to execute if true
	FalseBranch   []string `json:"false_branch"`  // Task IDs to execute if false
}

// Progress tracking structures

// WorkflowProgress represents the overall progress of a workflow execution
type WorkflowProgress struct {
	WorkflowExecutionID string             `json:"workflow_execution_id"`
	OverallProgress     float64            `json:"overall_progress"`     // 0.0 to 100.0
	CompletedTasks      int32              `json:"completed_tasks"`
	TotalTasks          int32              `json:"total_tasks"`
	FailedTasks         int32              `json:"failed_tasks"`
	CurrentTasks        []string           `json:"current_tasks"`        // Currently executing task IDs
	TaskProgress        map[string]float64 `json:"task_progress"`        // Task ID -> progress percentage
	EstimatedCompletion *time.Time         `json:"estimated_completion,omitempty"`
	LastUpdated         time.Time          `json:"last_updated"`
}

// DAG Analysis structures

// DAGNode represents a node in the workflow DAG for analysis
type DAGNode struct {
	ID           string              `json:"id"`
	Task         *Task               `json:"task"`
	Dependencies []*DAGNode          `json:"dependencies"`
	Dependents   []*DAGNode          `json:"dependents"`
	Level        int32               `json:"level"`        // Topological level (0 = no dependencies)
	CriticalPath bool                `json:"critical_path"` // Whether this node is on critical path
}

// DAGAnalysis represents analysis results of a workflow DAG
type DAGAnalysis struct {
	IsValid         bool        `json:"is_valid"`
	HasCycles       bool        `json:"has_cycles"`
	Cycles          [][]string  `json:"cycles,omitempty"`
	CriticalPath    []string    `json:"critical_path"`    // Task IDs in critical path
	MaxDepth        int32       `json:"max_depth"`
	ParallelLevels  [][]string  `json:"parallel_levels"`  // Tasks that can run in parallel
	EstimatedTime   int32       `json:"estimated_time"`   // Estimated execution time in seconds
}

// Helper methods

// IsCompleted returns true if the task is in a completed state
func (t *Task) IsCompleted() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusSkipped
}

// IsRunnable returns true if the task can be started (all dependencies completed)
func (t *Task) IsRunnable(completedTasks map[string]bool) bool {
	if t.Status != TaskStatusPending && t.Status != TaskStatusReady {
		return false
	}

	for _, depID := range t.Dependencies {
		if !completedTasks[depID] {
			return false
		}
	}

	return true
}

// Duration returns the execution duration of the task
func (t *Task) Duration() time.Duration {
	if t.StartedAt == nil || t.CompletedAt == nil {
		return 0
	}
	return t.CompletedAt.Sub(*t.StartedAt)
}

// IsCompleted returns true if the workflow execution is in a completed state
func (we *WorkflowExecution) IsCompleted() bool {
	return we.Status == WorkflowStatusCompleted || we.Status == WorkflowStatusFailed
}

// Duration returns the execution duration of the workflow
func (we *WorkflowExecution) Duration() time.Duration {
	if we.StartedAt == nil || we.CompletedAt == nil {
		return 0
	}
	return we.CompletedAt.Sub(*we.StartedAt)
}