package contracts

import "time"

// API Contracts for Microservices Communication
// These define the interfaces between services to ensure consistency

// WebSocket Service Contracts

type WebSocketConnection struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id,omitempty"`
	Status    string    `json:"status"`
	ConnectedAt time.Time `json:"connected_at"`
}

type WebSocketStats struct {
	TotalConnections    int                         `json:"total_connections"`
	ActiveConnections   int                         `json:"active_connections"`
	ConnectionsByUser   map[string]int              `json:"connections_by_user"`
	ConnectionsBySession map[string][]WebSocketConnection `json:"connections_by_session"`
}

type NotificationRequest struct {
	UserID    string      `json:"user_id"`
	SessionID string      `json:"session_id,omitempty"`
	Type      string      `json:"type"`
	Title     string      `json:"title"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Priority  string      `json:"priority,omitempty"` // low, normal, high, urgent
}

type BroadcastRequest struct {
	SessionID string      `json:"session_id"`
	Type      string      `json:"type"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	ExcludeUsers []string `json:"exclude_users,omitempty"`
}


// Examiner Service Contracts

type ValidationRequest struct {
	SessionID string                 `json:"session_id"`
	LabID     string                 `json:"lab_id"`
	UserID    string                 `json:"user_id"`
	Rules     []ValidationRule       `json:"rules"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

type ValidationRule struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"` // file_exists, command_output, port_open, etc.
	Description string            `json:"description"`
	Parameters  map[string]string `json:"parameters"`
	Weight      int               `json:"weight,omitempty"`
	Required    bool              `json:"required"`
}

type ValidationResponse struct {
	SessionID    string             `json:"session_id"`
	LabID        string             `json:"lab_id"`
	Valid        bool               `json:"valid"`
	Score        int                `json:"score"`
	MaxScore     int                `json:"max_score"`
	Results      []ValidationResult `json:"results"`
	CompletedAt  time.Time          `json:"completed_at"`
	Duration     time.Duration      `json:"duration"`
	Error        string             `json:"error,omitempty"`
}

type ValidationResult struct {
	RuleID      string            `json:"rule_id"`
	Passed      bool              `json:"passed"`
	Score       int               `json:"score"`
	MaxScore    int               `json:"max_score"`
	Message     string            `json:"message"`
	Details     map[string]string `json:"details,omitempty"`
	Evidence    string            `json:"evidence,omitempty"`
}

type FeedbackRequest struct {
	SessionID   string                 `json:"session_id"`
	UserID      string                 `json:"user_id"`
	LabID       string                 `json:"lab_id"`
	Results     []ValidationResult     `json:"results"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

type FeedbackResponse struct {
	SessionID   string              `json:"session_id"`
	Feedback    []FeedbackItem      `json:"feedback"`
	Suggestions []string            `json:"suggestions"`
	NextSteps   []string            `json:"next_steps"`
	Resources   []ResourceLink      `json:"resources,omitempty"`
}

type FeedbackItem struct {
	Type        string `json:"type"` // error, warning, info, success
	Title       string `json:"title"`
	Message     string `json:"message"`
	RuleID      string `json:"rule_id,omitempty"`
	ActionItem  string `json:"action_item,omitempty"`
}

type ResourceLink struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Type        string `json:"type"` // documentation, tutorial, example
}

// Workflow Service Contracts

type WorkflowRequest struct {
	Name        string                 `json:"name"`
	SessionID   string                 `json:"session_id"`
	UserID      string                 `json:"user_id"`
	Steps       []WorkflowStep         `json:"steps"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Priority    int                    `json:"priority,omitempty"`
}

type WorkflowStep struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"` // script, validation, notification, etc.
	Command      string            `json:"command,omitempty"`
	Parameters   map[string]string `json:"parameters,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Timeout      time.Duration     `json:"timeout,omitempty"`
	Retries      int               `json:"retries,omitempty"`
}

type WorkflowResponse struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	SessionID   string        `json:"session_id"`
	Status      string        `json:"status"` // pending, running, completed, failed, cancelled
	Progress    int           `json:"progress"` // 0-100
	CurrentStep string        `json:"current_step"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Duration    time.Duration `json:"duration"`
	Error       string        `json:"error,omitempty"`
}

type WorkflowStepResult struct {
	StepID      string            `json:"step_id"`
	Status      string            `json:"status"`
	Output      string            `json:"output,omitempty"`
	Error       string            `json:"error,omitempty"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Duration    time.Duration     `json:"duration"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type WorkflowExecutionDetails struct {
	Workflow WorkflowResponse     `json:"workflow"`
	Steps    []WorkflowStepResult `json:"steps"`
	Logs     []string             `json:"logs,omitempty"`
}

// Worker Service Contracts (for background jobs)

type JobRequest struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Priority    int                    `json:"priority,omitempty"`
	Delay       time.Duration          `json:"delay,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
}

type JobResponse struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Status      string        `json:"status"` // queued, running, completed, failed, cancelled
	Progress    int           `json:"progress"`
	Result      interface{}   `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	StartedAt   *time.Time    `json:"started_at,omitempty"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Duration    time.Duration `json:"duration"`
	Retries     int           `json:"retries"`
}

// Common Response Structures

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

type Pagination struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// Error Response Structure

type ErrorResponse struct {
	Error   string            `json:"error"`
	Code    string            `json:"code,omitempty"`
	Details map[string]string `json:"details,omitempty"`
	TraceID string            `json:"trace_id,omitempty"`
}

// Health Check Structure

type HealthResponse struct {
	Status      string            `json:"status"` // healthy, unhealthy, degraded
	Version     string            `json:"version"`
	Timestamp   time.Time         `json:"timestamp"`
	Uptime      time.Duration     `json:"uptime"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}