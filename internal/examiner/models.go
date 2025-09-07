package examiner

import (
	"encoding/json"
	"time"
)

// ValidationRule represents a rule for validating lab solutions
type ValidationRule struct {
	ID          string          `json:"id" gorm:"primary_key"`
	LabID       string          `json:"lab_id" gorm:"index;not null"`
	TaskID      string          `json:"task_id" gorm:"index;not null"`
	Name        string          `json:"name" gorm:"not null"`
	Description string          `json:"description"`
	Type        ValidationType  `json:"type" gorm:"not null"`
	Condition   RuleCondition   `json:"condition" gorm:"type:jsonb"`
	Weight      float64         `json:"weight" gorm:"default:1.0"`         // Weight for scoring
	Timeout     int32           `json:"timeout" gorm:"default:30"`         // Timeout in seconds
	Order       int32           `json:"order" gorm:"default:0"`            // Execution order
	Required    bool            `json:"required" gorm:"default:true"`       // Required for task completion
	Enabled     bool            `json:"enabled" gorm:"default:true"`
	CreatedAt   time.Time       `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time       `json:"updated_at" gorm:"autoUpdateTime"`
}

// ValidationType represents different types of validation
type ValidationType string

const (
	// File-based validations
	ValidationTypeFileExists     ValidationType = "file_exists"
	ValidationTypeFileContent    ValidationType = "file_content"
	ValidationTypeFileSize       ValidationType = "file_size"
	ValidationTypeFilePermissions ValidationType = "file_permissions"
	ValidationTypeDirectoryExists ValidationType = "directory_exists"

	// Command-based validations
	ValidationTypeCommandOutput   ValidationType = "command_output"
	ValidationTypeCommandExitCode ValidationType = "command_exit_code"
	ValidationTypeProcessRunning  ValidationType = "process_running"
	ValidationTypePortOpen        ValidationType = "port_open"

	// Content-based validations
	ValidationTypeRegexMatch      ValidationType = "regex_match"
	ValidationTypeJSONStructure   ValidationType = "json_structure"
	ValidationTypeXMLStructure    ValidationType = "xml_structure"
	ValidationTypeYAMLStructure   ValidationType = "yaml_structure"

	// Network-based validations
	ValidationTypeHTTPResponse    ValidationType = "http_response"
	ValidationTypeHTTPStatusCode  ValidationType = "http_status_code"
	ValidationTypeHTTPHeaders     ValidationType = "http_headers"
	ValidationTypeDNSResolution   ValidationType = "dns_resolution"

	// Database validations
	ValidationTypeDatabaseQuery   ValidationType = "database_query"
	ValidationTypeTableExists     ValidationType = "table_exists"
	ValidationTypeRecordCount     ValidationType = "record_count"

	// Custom script validations
	ValidationTypeCustomScript   ValidationType = "custom_script"
	ValidationTypeCustomFunction ValidationType = "custom_function"
)

// RuleCondition represents the condition for a validation rule
type RuleCondition struct {
	// File validation fields
	FilePath    string `json:"file_path,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Expected    string `json:"expected,omitempty"`
	MinSize     int64  `json:"min_size,omitempty"`
	MaxSize     int64  `json:"max_size,omitempty"`
	Permissions string `json:"permissions,omitempty"`

	// Command validation fields
	Command     string   `json:"command,omitempty"`
	Arguments   []string `json:"arguments,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	ExitCode    int      `json:"exit_code,omitempty"`
	ProcessName string   `json:"process_name,omitempty"`
	Port        int      `json:"port,omitempty"`

	// Content validation fields
	Regex       string                 `json:"regex,omitempty"`
	JSONSchema  map[string]interface{} `json:"json_schema,omitempty"`
	XMLSchema   string                 `json:"xml_schema,omitempty"`
	YAMLSchema  string                 `json:"yaml_schema,omitempty"`

	// Network validation fields
	URL            string            `json:"url,omitempty"`
	Method         string            `json:"method,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	RequestBody    string            `json:"request_body,omitempty"`
	StatusCode     int               `json:"status_code,omitempty"`
	ResponseBody   string            `json:"response_body,omitempty"`
	Domain         string            `json:"domain,omitempty"`
	ExpectedIP     string            `json:"expected_ip,omitempty"`

	// Database validation fields
	Query         string `json:"query,omitempty"`
	Database      string `json:"database,omitempty"`
	Table         string `json:"table,omitempty"`
	ExpectedCount int    `json:"expected_count,omitempty"`

	// Script validation fields
	Script     string                 `json:"script,omitempty"`
	Language   string                 `json:"language,omitempty"`
	Function   string                 `json:"function,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`

	// General fields
	CaseSensitive bool                   `json:"case_sensitive,omitempty"`
	Negate        bool                   `json:"negate,omitempty"`
	Custom        map[string]interface{} `json:"custom,omitempty"`
}

// ValidationResult represents the result of a validation
type ValidationResult struct {
	ID           string            `json:"id" gorm:"primary_key"`
	SessionID    string            `json:"session_id" gorm:"index;not null"`
	UserID       string            `json:"user_id" gorm:"index;not null"`
	LabID        string            `json:"lab_id" gorm:"index;not null"`
	TaskID       string            `json:"task_id" gorm:"index;not null"`
	RuleID       string            `json:"rule_id" gorm:"index;not null"`
	Status       ValidationStatus  `json:"status" gorm:"not null"`
	Passed       bool              `json:"passed" gorm:"not null"`
	Score        float64           `json:"score" gorm:"default:0"`
	MaxScore     float64           `json:"max_score" gorm:"default:1"`
	Message      string            `json:"message"`
	Details      ResultDetails     `json:"details" gorm:"type:jsonb"`
	ExecutionTime int32            `json:"execution_time"` // Time in milliseconds
	StartedAt    time.Time         `json:"started_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at" gorm:"autoCreateTime"`
}

// ValidationStatus represents the status of a validation
type ValidationStatus string

const (
	ValidationStatusPending   ValidationStatus = "pending"
	ValidationStatusRunning   ValidationStatus = "running"
	ValidationStatusCompleted ValidationStatus = "completed"
	ValidationStatusFailed    ValidationStatus = "failed"
	ValidationStatusTimeout   ValidationStatus = "timeout"
	ValidationStatusSkipped   ValidationStatus = "skipped"
)

// ResultDetails contains detailed information about validation results
type ResultDetails struct {
	// General result info
	ActualValue   interface{} `json:"actual_value,omitempty"`
	ExpectedValue interface{} `json:"expected_value,omitempty"`
	Comparison    string      `json:"comparison,omitempty"`

	// File validation details
	FileInfo *FileInfo `json:"file_info,omitempty"`

	// Command validation details
	CommandOutput *CommandOutput `json:"command_output,omitempty"`

	// HTTP validation details
	HTTPResponse *HTTPResponse `json:"http_response,omitempty"`

	// Database validation details
	DatabaseResult *DatabaseResult `json:"database_result,omitempty"`

	// Script validation details
	ScriptResult *ScriptResult `json:"script_result,omitempty"`

	// Error information
	Error       string `json:"error,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	StackTrace  string `json:"stack_trace,omitempty"`
	Suggestions string `json:"suggestions,omitempty"`
}

// FileInfo contains file validation details
type FileInfo struct {
	Exists      bool   `json:"exists"`
	Size        int64  `json:"size"`
	Permissions string `json:"permissions"`
	ModTime     string `json:"mod_time"`
	IsDir       bool   `json:"is_dir"`
	Content     string `json:"content,omitempty"`
	Hash        string `json:"hash,omitempty"`
}

// CommandOutput contains command execution details
type CommandOutput struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Duration   int64  `json:"duration"` // Duration in milliseconds
	TimedOut   bool   `json:"timed_out"`
}

// HTTPResponse contains HTTP request details
type HTTPResponse struct {
	StatusCode    int               `json:"status_code"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body,omitempty"`
	ContentLength int64             `json:"content_length"`
	Duration      int64             `json:"duration"` // Duration in milliseconds
}

// DatabaseResult contains database query details
type DatabaseResult struct {
	RowCount int                      `json:"row_count"`
	Columns  []string                 `json:"columns"`
	Rows     []map[string]interface{} `json:"rows,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

// ScriptResult contains custom script execution details
type ScriptResult struct {
	Output     interface{} `json:"output"`
	ReturnCode int         `json:"return_code"`
	Duration   int64       `json:"duration"`
	Error      string      `json:"error,omitempty"`
}

// TaskValidation represents validation status for a complete task
type TaskValidation struct {
	ID              string              `json:"id" gorm:"primary_key"`
	SessionID       string              `json:"session_id" gorm:"index;not null"`
	UserID          string              `json:"user_id" gorm:"index;not null"`
	LabID           string              `json:"lab_id" gorm:"index;not null"`
	TaskID          string              `json:"task_id" gorm:"index;not null"`
	Status          TaskValidationStatus `json:"status" gorm:"not null"`
	TotalRules      int32               `json:"total_rules"`
	PassedRules     int32               `json:"passed_rules"`
	FailedRules     int32               `json:"failed_rules"`
	SkippedRules    int32               `json:"skipped_rules"`
	Score           float64             `json:"score"`
	MaxScore        float64             `json:"max_score"`
	Percentage      float64             `json:"percentage"`
	CompletionTime  int32               `json:"completion_time"` // Time in seconds
	Feedback        TaskFeedback        `json:"feedback" gorm:"type:jsonb"`
	Attempts        int32               `json:"attempts" gorm:"default:1"`
	LastAttemptAt   time.Time           `json:"last_attempt_at"`
	CreatedAt       time.Time           `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time           `json:"updated_at" gorm:"autoUpdateTime"`
}

// TaskValidationStatus represents the overall status of task validation
type TaskValidationStatus string

const (
	TaskValidationStatusPending    TaskValidationStatus = "pending"
	TaskValidationStatusRunning    TaskValidationStatus = "running"
	TaskValidationStatusCompleted  TaskValidationStatus = "completed"
	TaskValidationStatusPartial    TaskValidationStatus = "partial"
	TaskValidationStatusFailed     TaskValidationStatus = "failed"
	TaskValidationStatusIncomplete TaskValidationStatus = "incomplete"
)

// TaskFeedback provides feedback for task completion
type TaskFeedback struct {
	Summary        string             `json:"summary"`
	Strengths      []string           `json:"strengths"`
	Improvements   []string           `json:"improvements"`
	NextSteps      []string           `json:"next_steps"`
	Resources      []FeedbackResource `json:"resources"`
	Grade          string             `json:"grade,omitempty"`
	Comments       string             `json:"comments,omitempty"`
	AutoGenerated  bool               `json:"auto_generated"`
}

// FeedbackResource provides additional learning resources
type FeedbackResource struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Type        string `json:"type"` // documentation, tutorial, video, etc.
	Description string `json:"description"`
}

// LabValidation represents validation status for an entire lab
type LabValidation struct {
	ID              string             `json:"id" gorm:"primary_key"`
	SessionID       string             `json:"session_id" gorm:"uniqueIndex;not null"`
	UserID          string             `json:"user_id" gorm:"index;not null"`
	LabID           string             `json:"lab_id" gorm:"index;not null"`
	Status          LabValidationStatus `json:"status" gorm:"not null"`
	TotalTasks      int32              `json:"total_tasks"`
	CompletedTasks  int32              `json:"completed_tasks"`
	FailedTasks     int32              `json:"failed_tasks"`
	PartialTasks    int32              `json:"partial_tasks"`
	TotalScore      float64            `json:"total_score"`
	MaxScore        float64            `json:"max_score"`
	FinalPercentage float64            `json:"final_percentage"`
	Grade           string             `json:"grade"`
	StartedAt       time.Time          `json:"started_at"`
	CompletedAt     *time.Time         `json:"completed_at,omitempty"`
	Duration        int32              `json:"duration"` // Duration in minutes
	Feedback        LabFeedback        `json:"feedback" gorm:"type:jsonb"`
	Certificate     *Certificate       `json:"certificate,omitempty" gorm:"type:jsonb"`
	CreatedAt       time.Time          `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time          `json:"updated_at" gorm:"autoUpdateTime"`
}

// LabValidationStatus represents the overall status of lab validation
type LabValidationStatus string

const (
	LabValidationStatusInProgress LabValidationStatus = "in_progress"
	LabValidationStatusCompleted  LabValidationStatus = "completed"
	LabValidationStatusFailed     LabValidationStatus = "failed"
	LabValidationStatusAbandoned  LabValidationStatus = "abandoned"
)

// LabFeedback provides comprehensive feedback for lab completion
type LabFeedback struct {
	OverallSummary    string                      `json:"overall_summary"`
	TaskSummaries     map[string]string           `json:"task_summaries"`
	SkillsAssessed    []SkillAssessment          `json:"skills_assessed"`
	LearningObjectives map[string]ObjectiveStatus `json:"learning_objectives"`
	Recommendations   []string                   `json:"recommendations"`
	TimeAnalysis      TimeAnalysis               `json:"time_analysis"`
	DifficultyRating  int                        `json:"difficulty_rating"` // 1-10
	InstructorNotes   string                     `json:"instructor_notes,omitempty"`
}

// SkillAssessment represents assessment of a specific skill
type SkillAssessment struct {
	Skill       string  `json:"skill"`
	Level       string  `json:"level"`       // Beginner, Intermediate, Advanced
	Proficiency float64 `json:"proficiency"` // 0-100
	Evidence    string  `json:"evidence"`
}

// ObjectiveStatus represents completion status of learning objectives
type ObjectiveStatus struct {
	Achieved    bool    `json:"achieved"`
	Proficiency float64 `json:"proficiency"`
	Evidence    string  `json:"evidence"`
}

// TimeAnalysis provides analysis of time spent on tasks
type TimeAnalysis struct {
	TotalTime       int32            `json:"total_time"`       // Minutes
	AverageTime     float64          `json:"average_time"`     // Minutes per task
	FastestTask     string           `json:"fastest_task"`
	SlowestTask     string           `json:"slowest_task"`
	TimePerTask     map[string]int32 `json:"time_per_task"`
	EfficiencyScore float64          `json:"efficiency_score"` // 0-100
}

// Certificate represents a completion certificate
type Certificate struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Recipient   string    `json:"recipient"`
	Issuer      string    `json:"issuer"`
	IssuedDate  time.Time `json:"issued_date"`
	ExpiryDate  *time.Time `json:"expiry_date,omitempty"`
	URL         string    `json:"url"`
	Hash        string    `json:"hash"`        // For verification
	Blockchain  bool      `json:"blockchain"`  // Stored on blockchain
}

// ValidationTemplate represents a reusable validation template
type ValidationTemplate struct {
	ID          string                 `json:"id" gorm:"primary_key"`
	Name        string                 `json:"name" gorm:"not null"`
	Description string                 `json:"description"`
	Category    string                 `json:"category"`
	Language    string                 `json:"language,omitempty"`
	Framework   string                 `json:"framework,omitempty"`
	Rules       []ValidationRule       `json:"rules" gorm:"-"` // Loaded separately
	Metadata    map[string]interface{} `json:"metadata" gorm:"type:jsonb"`
	CreatedBy   string                 `json:"created_by"`
	CreatedAt   time.Time              `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time              `json:"updated_at" gorm:"autoUpdateTime"`
}

// ValidationMetrics provides metrics about validation system
type ValidationMetrics struct {
	TotalValidations   int64            `json:"total_validations"`
	SuccessRate        float64          `json:"success_rate"`
	AverageScore       float64          `json:"average_score"`
	ValidationsByType  map[string]int64 `json:"validations_by_type"`
	ValidationsByStatus map[string]int64 `json:"validations_by_status"`
	AverageExecutionTime float64        `json:"average_execution_time"`
	TopFailedRules     []RuleFailure    `json:"top_failed_rules"`
	PerformanceStats   PerformanceStats `json:"performance_stats"`
}

// RuleFailure represents a frequently failing rule
type RuleFailure struct {
	RuleID      string  `json:"rule_id"`
	RuleName    string  `json:"rule_name"`
	FailureCount int64  `json:"failure_count"`
	FailureRate float64 `json:"failure_rate"`
}

// PerformanceStats provides performance statistics
type PerformanceStats struct {
	FastestValidation  int64   `json:"fastest_validation"`  // milliseconds
	SlowestValidation  int64   `json:"slowest_validation"`  // milliseconds
	MedianExecutionTime float64 `json:"median_execution_time"` // milliseconds
	P95ExecutionTime   float64 `json:"p95_execution_time"`    // milliseconds
	TimeoutRate        float64 `json:"timeout_rate"`
}

// Helper methods

// IsCompleted returns true if the task validation is completed
func (tv *TaskValidation) IsCompleted() bool {
	return tv.Status == TaskValidationStatusCompleted
}

// IsSuccessful returns true if the task validation was successful
func (tv *TaskValidation) IsSuccessful() bool {
	return tv.Status == TaskValidationStatusCompleted && tv.Percentage >= 70.0 // 70% pass threshold
}

// GetGrade returns a letter grade based on the percentage
func (tv *TaskValidation) GetGrade() string {
	switch {
	case tv.Percentage >= 90:
		return "A"
	case tv.Percentage >= 80:
		return "B"
	case tv.Percentage >= 70:
		return "C"
	case tv.Percentage >= 60:
		return "D"
	default:
		return "F"
	}
}

// IsCompleted returns true if the lab validation is completed
func (lv *LabValidation) IsCompleted() bool {
	return lv.Status == LabValidationStatusCompleted
}

// IsSuccessful returns true if the lab validation was successful
func (lv *LabValidation) IsSuccessful() bool {
	return lv.Status == LabValidationStatusCompleted && lv.FinalPercentage >= 70.0
}

// GetOverallGrade returns overall lab grade
func (lv *LabValidation) GetOverallGrade() string {
	switch {
	case lv.FinalPercentage >= 90:
		return "A"
	case lv.FinalPercentage >= 80:
		return "B"
	case lv.FinalPercentage >= 70:
		return "C"
	case lv.FinalPercentage >= 60:
		return "D"
	default:
		return "F"
	}
}

// IsPassed returns true if the validation result passed
func (vr *ValidationResult) IsPassed() bool {
	return vr.Passed && vr.Status == ValidationStatusCompleted
}