package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskExecutor defines the interface for task execution
type TaskExecutor interface {
	Execute(ctx context.Context, task *Task, execution *TaskExecution) error
	GetType() TaskType
}

// WorkflowOrchestrator manages workflow execution
type WorkflowOrchestrator struct {
	executors          map[TaskType]TaskExecutor
	executionManager   *ExecutionManager
	dependencyManager  *TaskDependencyManager
	progressTracker    *ProgressTracker
	maxConcurrentTasks int
	mu                 sync.RWMutex
}

// NewWorkflowOrchestrator creates a new workflow orchestrator
func NewWorkflowOrchestrator(maxConcurrentTasks int) *WorkflowOrchestrator {
	orchestrator := &WorkflowOrchestrator{
		executors:          make(map[TaskType]TaskExecutor),
		maxConcurrentTasks: maxConcurrentTasks,
	}

	// Register default task executors
	orchestrator.RegisterExecutor(NewCommandTaskExecutor())
	orchestrator.RegisterExecutor(NewValidationTaskExecutor())
	orchestrator.RegisterExecutor(NewFileTaskExecutor())
	orchestrator.RegisterExecutor(NewHTTPTaskExecutor())
	orchestrator.RegisterExecutor(NewManualTaskExecutor())
	orchestrator.RegisterExecutor(NewConditionalTaskExecutor())

	return orchestrator
}

// RegisterExecutor registers a task executor for a specific task type
func (wo *WorkflowOrchestrator) RegisterExecutor(executor TaskExecutor) {
	wo.mu.Lock()
	defer wo.mu.Unlock()
	wo.executors[executor.GetType()] = executor
}

// ExecuteWorkflow starts workflow execution
func (wo *WorkflowOrchestrator) ExecuteWorkflow(ctx context.Context, workflowExecution *WorkflowExecution, workflow *Workflow) error {
	log.Printf("Starting workflow execution %s for workflow %s", workflowExecution.ID, workflow.ID)

	// Build DAG and dependency manager
	dagManager := NewDAGManager()
	analysis, nodes, err := dagManager.BuildDAG(workflow)
	if err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	if !analysis.IsValid {
		return fmt.Errorf("workflow DAG is invalid: %v", analysis.Cycles)
	}

	wo.dependencyManager = NewTaskDependencyManager(nodes)
	wo.progressTracker = NewProgressTracker(wo.dependencyManager)

	// Initialize execution manager
	wo.executionManager = NewExecutionManager()

	// Start workflow execution
	workflowExecution.Status = WorkflowStatusRunning
	now := time.Now()
	workflowExecution.StartedAt = &now

	// Execute workflow
	err = wo.executeWorkflowTasks(ctx, workflowExecution, workflow)
	
	// Update final status
	now = time.Now()
	workflowExecution.CompletedAt = &now
	
	if err != nil {
		workflowExecution.Status = WorkflowStatusFailed
		return err
	}

	workflowExecution.Status = WorkflowStatusCompleted
	log.Printf("Completed workflow execution %s", workflowExecution.ID)
	return nil
}

// executeWorkflowTasks executes tasks according to DAG dependencies
func (wo *WorkflowOrchestrator) executeWorkflowTasks(ctx context.Context, workflowExecution *WorkflowExecution, workflow *Workflow) error {
	taskExecutions := make(map[string]*TaskExecution)
	runningTasks := make(map[string]context.CancelFunc)
	var wg sync.WaitGroup

	// Main execution loop
	for {
		// Check if workflow is completed
		if wo.isWorkflowComplete(taskExecutions, workflow) {
			break
		}

		// Get runnable tasks
		runnableTasks := wo.dependencyManager.GetRunnableTasks(taskExecutions)
		
		// Limit concurrent tasks
		if len(runningTasks) >= wo.maxConcurrentTasks {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Start new tasks
		tasksStarted := false
		for _, task := range runnableTasks {
			if len(runningTasks) >= wo.maxConcurrentTasks {
				break
			}

			if _, isRunning := runningTasks[task.ID]; isRunning {
				continue
			}

			// Create task execution
			taskExecution := &TaskExecution{
				ID:                  uuid.New().String(),
				WorkflowExecutionID: workflowExecution.ID,
				TaskID:              task.ID,
				Status:              TaskStatusRunning,
				CreatedAt:           time.Now(),
			}

			now := time.Now()
			taskExecution.StartedAt = &now
			taskExecutions[task.ID] = taskExecution

			// Start task execution
			taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.Timeout)*time.Second)
			runningTasks[task.ID] = cancel

			wg.Add(1)
			go func(t *Task, exec *TaskExecution) {
				defer wg.Done()
				defer func() {
					wo.mu.Lock()
					delete(runningTasks, t.ID)
					wo.mu.Unlock()
				}()

				err := wo.executeTask(taskCtx, t, exec)
				
				now := time.Now()
				exec.CompletedAt = &now
				exec.UpdatedAt = now

				if err != nil {
					exec.Status = TaskStatusFailed
					exec.ErrorMessage = err.Error()
					log.Printf("Task %s failed: %v", t.ID, err)
				} else {
					exec.Status = TaskStatusCompleted
					log.Printf("Task %s completed successfully", t.ID)
				}
			}(task, taskExecution)

			tasksStarted = true
		}

		// If no new tasks started and no tasks running, check for blocked tasks
		if !tasksStarted && len(runningTasks) == 0 {
			blockedTasks := wo.dependencyManager.GetBlockedTasks(taskExecutions)
			if len(blockedTasks) > 0 {
				return fmt.Errorf("workflow blocked: tasks %v cannot proceed due to failed dependencies", 
					wo.getTaskIDs(blockedTasks))
			}
			
			// No runnable tasks and none running - workflow might be stuck
			if !wo.isWorkflowComplete(taskExecutions, workflow) {
				return fmt.Errorf("workflow appears to be stuck - no runnable or running tasks")
			}
		}

		// Small delay to prevent busy waiting
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all tasks to complete
	wg.Wait()

	// Check if workflow completed successfully
	if wo.hasFailedTasks(taskExecutions) {
		return fmt.Errorf("workflow failed due to task failures")
	}

	return nil
}

// executeTask executes a single task
func (wo *WorkflowOrchestrator) executeTask(ctx context.Context, task *Task, execution *TaskExecution) error {
	executor, exists := wo.executors[task.Type]
	if !exists {
		return fmt.Errorf("no executor found for task type: %s", task.Type)
	}

	// Execute with retry logic
	var lastError error
	for attempt := int32(0); attempt <= task.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retrying task %s, attempt %d/%d", task.ID, attempt+1, task.MaxRetries+1)
			execution.Retries = attempt
		}

		err := executor.Execute(ctx, task, execution)
		if err == nil {
			return nil
		}

		lastError = err
		
		// Check if context is cancelled (timeout or parent cancellation)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Wait before retry
		if attempt < task.MaxRetries {
			retryDelay := time.Duration(attempt+1) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
				// Continue to next retry
			}
		}
	}

	return lastError
}

// Helper methods

func (wo *WorkflowOrchestrator) isWorkflowComplete(taskExecutions map[string]*TaskExecution, workflow *Workflow) bool {
	for _, task := range workflow.Tasks {
		execution, exists := taskExecutions[task.ID]
		if !exists || (execution.Status != TaskStatusCompleted && execution.Status != TaskStatusFailed) {
			return false
		}
	}
	return true
}

func (wo *WorkflowOrchestrator) hasFailedTasks(taskExecutions map[string]*TaskExecution) bool {
	for _, execution := range taskExecutions {
		if execution.Status == TaskStatusFailed {
			return true
		}
	}
	return false
}

func (wo *WorkflowOrchestrator) getTaskIDs(tasks []*Task) []string {
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}

// Task Executors Implementation

// CommandTaskExecutor executes shell commands
type CommandTaskExecutor struct{}

func NewCommandTaskExecutor() *CommandTaskExecutor {
	return &CommandTaskExecutor{}
}

func (cte *CommandTaskExecutor) GetType() TaskType {
	return TaskTypeCommand
}

func (cte *CommandTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config CommandTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid command task configuration: %w", err)
	}

	// Create command
	parts := strings.Fields(config.Command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	
	// Set working directory
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}

	// Set environment variables
	if len(config.Environment) > 0 {
		env := cmd.Env
		for key, value := range config.Environment {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = env
	}

	// Execute command
	output, err := cmd.CombinedOutput()
	
	// Store result
	result := map[string]interface{}{
		"command":     config.Command,
		"output":      string(output),
		"exit_code":   cmd.ProcessState.ExitCode(),
		"working_dir": config.WorkingDir,
	}

	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	return err
}

// ValidationTaskExecutor validates conditions
type ValidationTaskExecutor struct{}

func NewValidationTaskExecutor() *ValidationTaskExecutor {
	return &ValidationTaskExecutor{}
}

func (vte *ValidationTaskExecutor) GetType() TaskType {
	return TaskTypeValidation
}

func (vte *ValidationTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config ValidationTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid validation task configuration: %w", err)
	}

	var result map[string]interface{}
	var err error

	switch config.Type {
	case "file_exists":
		result, err = vte.validateFileExists(config.Target)
	case "command_output":
		result, err = vte.validateCommandOutput(ctx, config.Target, config.Expected)
	default:
		return fmt.Errorf("unsupported validation type: %s", config.Type)
	}

	// Store result
	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	return err
}

func (vte *ValidationTaskExecutor) validateFileExists(filePath string) (map[string]interface{}, error) {
	// Implementation would check if file exists
	result := map[string]interface{}{
		"type":   "file_exists",
		"target": filePath,
		"exists": true, // Placeholder
	}
	return result, nil
}

func (vte *ValidationTaskExecutor) validateCommandOutput(ctx context.Context, command string, expected json.RawMessage) (map[string]interface{}, error) {
	// Implementation would execute command and validate output
	result := map[string]interface{}{
		"type":     "command_output",
		"command":  command,
		"expected": string(expected),
		"actual":   "placeholder", // Placeholder
		"matches":  true,          // Placeholder
	}
	return result, nil
}

// FileTaskExecutor handles file operations
type FileTaskExecutor struct{}

func NewFileTaskExecutor() *FileTaskExecutor {
	return &FileTaskExecutor{}
}

func (fte *FileTaskExecutor) GetType() TaskType {
	return TaskTypeFile
}

func (fte *FileTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config FileTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid file task configuration: %w", err)
	}

	result := map[string]interface{}{
		"operation": config.Operation,
		"path":      config.Path,
		"success":   true, // Placeholder
	}

	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	// Placeholder implementation
	return nil
}

// HTTPTaskExecutor handles HTTP requests
type HTTPTaskExecutor struct{}

func NewHTTPTaskExecutor() *HTTPTaskExecutor {
	return &HTTPTaskExecutor{}
}

func (hte *HTTPTaskExecutor) GetType() TaskType {
	return TaskTypeHTTP
}

func (hte *HTTPTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config HTTPTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid HTTP task configuration: %w", err)
	}

	result := map[string]interface{}{
		"method":      config.Method,
		"url":         config.URL,
		"status_code": 200, // Placeholder
		"success":     true,
	}

	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	// Placeholder implementation
	return nil
}

// ManualTaskExecutor handles manual tasks
type ManualTaskExecutor struct{}

func NewManualTaskExecutor() *ManualTaskExecutor {
	return &ManualTaskExecutor{}
}

func (mte *ManualTaskExecutor) GetType() TaskType {
	return TaskTypeManual
}

func (mte *ManualTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config ManualTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid manual task configuration: %w", err)
	}

	result := map[string]interface{}{
		"instructions":      config.Instructions,
		"validation_steps":  config.ValidationSteps,
		"awaiting_user":     true,
		"completed_by_user": false, // Will be updated by user interaction
	}

	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	// Manual tasks require user interaction - return success for now
	// In real implementation, this would wait for user confirmation
	return nil
}

// ConditionalTaskExecutor handles conditional branching
type ConditionalTaskExecutor struct{}

func NewConditionalTaskExecutor() *ConditionalTaskExecutor {
	return &ConditionalTaskExecutor{}
}

func (cte *ConditionalTaskExecutor) GetType() TaskType {
	return TaskTypeConditional
}

func (cte *ConditionalTaskExecutor) Execute(ctx context.Context, task *Task, execution *TaskExecution) error {
	var config ConditionalTaskConfig
	if err := json.Unmarshal(task.Configuration, &config); err != nil {
		return fmt.Errorf("invalid conditional task configuration: %w", err)
	}

	// Evaluate condition (placeholder implementation)
	conditionResult := true // Placeholder

	result := map[string]interface{}{
		"condition":       config.Condition,
		"condition_result": conditionResult,
		"executed_branch":  "true_branch", // or "false_branch"
	}

	resultJSON, _ := json.Marshal(result)
	execution.Result = resultJSON

	return nil
}

// ExecutionManager manages task execution state
type ExecutionManager struct {
	executions map[string]*TaskExecution
	mu         sync.RWMutex
}

// NewExecutionManager creates a new execution manager
func NewExecutionManager() *ExecutionManager {
	return &ExecutionManager{
		executions: make(map[string]*TaskExecution),
	}
}

// GetExecution gets a task execution by task ID
func (em *ExecutionManager) GetExecution(taskID string) (*TaskExecution, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()
	execution, exists := em.executions[taskID]
	return execution, exists
}

// SetExecution sets a task execution
func (em *ExecutionManager) SetExecution(taskID string, execution *TaskExecution) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.executions[taskID] = execution
}

// GetAllExecutions returns all task executions
func (em *ExecutionManager) GetAllExecutions() map[string]*TaskExecution {
	em.mu.RLock()
	defer em.mu.RUnlock()
	executions := make(map[string]*TaskExecution, len(em.executions))
	for k, v := range em.executions {
		executions[k] = v
	}
	return executions
}