package workflow

import (
	"time"
)

// ProgressTracker tracks workflow progress through task graphs
type ProgressTracker struct {
	dependencyManager *TaskDependencyManager
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(dependencyManager *TaskDependencyManager) *ProgressTracker {
	return &ProgressTracker{
		dependencyManager: dependencyManager,
	}
}

// CalculateDetailedProgress calculates comprehensive workflow progress
func (pt *ProgressTracker) CalculateDetailedProgress(workflowExecution *WorkflowExecution, taskExecutions map[string]*TaskExecution) *WorkflowProgress {
	totalTasks := int32(len(pt.dependencyManager.nodes))
	completedTasks := int32(0)
	failedTasks := int32(0)
	runningTasks := make([]string, 0)
	taskProgress := make(map[string]float64)

	// Calculate task-level progress
	for taskID := range pt.dependencyManager.nodes {
		progress := pt.calculateTaskProgress(taskID, taskExecutions)
		taskProgress[taskID] = progress

		if execution, exists := taskExecutions[taskID]; exists {
			switch execution.Status {
			case TaskStatusCompleted:
				completedTasks++
			case TaskStatusFailed:
				failedTasks++
			case TaskStatusRunning:
				runningTasks = append(runningTasks, taskID)
			}
		}
	}

	// Calculate weighted overall progress based on task complexity
	overallProgress := pt.calculateWeightedProgress(taskProgress, pt.dependencyManager.nodes)

	// Estimate completion time
	estimatedCompletion := pt.estimateCompletionTime(workflowExecution, taskExecutions, overallProgress)

	return &WorkflowProgress{
		WorkflowExecutionID: workflowExecution.ID,
		OverallProgress:     overallProgress,
		CompletedTasks:      completedTasks,
		TotalTasks:          totalTasks,
		FailedTasks:         failedTasks,
		CurrentTasks:        runningTasks,
		TaskProgress:        taskProgress,
		EstimatedCompletion: estimatedCompletion,
		LastUpdated:         time.Now(),
	}
}

// calculateTaskProgress calculates progress for an individual task
func (pt *ProgressTracker) calculateTaskProgress(taskID string, taskExecutions map[string]*TaskExecution) float64 {
	execution, exists := taskExecutions[taskID]
	if !exists {
		return 0.0
	}

	switch execution.Status {
	case TaskStatusCompleted:
		return 100.0
	case TaskStatusFailed, TaskStatusSkipped:
		return 0.0
	case TaskStatusRunning:
		return pt.calculateRunningTaskProgress(execution)
	case TaskStatusReady:
		return 5.0 // Small progress for ready tasks
	default:
		return 0.0
	}
}

// calculateRunningTaskProgress estimates progress for running tasks
func (pt *ProgressTracker) calculateRunningTaskProgress(execution *TaskExecution) float64 {
	if execution.StartedAt == nil {
		return 10.0
	}

	// Get the associated task to determine timeout
	node, exists := pt.dependencyManager.nodes[execution.TaskID]
	if !exists {
		return 50.0 // Default for running tasks
	}

	elapsed := time.Since(*execution.StartedAt)
	timeout := time.Duration(node.Task.Timeout) * time.Second

	// Calculate progress based on elapsed time vs timeout
	timeProgress := float64(elapsed) / float64(timeout) * 100.0
	
	// Cap at 90% to indicate it's not complete
	if timeProgress > 90.0 {
		return 90.0
	}
	
	// Minimum 10% for started tasks
	if timeProgress < 10.0 {
		return 10.0
	}

	return timeProgress
}

// calculateWeightedProgress calculates overall progress with task complexity weights
func (pt *ProgressTracker) calculateWeightedProgress(taskProgress map[string]float64, nodes map[string]*DAGNode) float64 {
	if len(nodes) == 0 {
		return 100.0
	}

	totalWeight := 0.0
	weightedProgress := 0.0

	for taskID, node := range nodes {
		weight := pt.calculateTaskWeight(node)
		progress := taskProgress[taskID]
		
		totalWeight += weight
		weightedProgress += weight * progress
	}

	if totalWeight == 0 {
		return 0.0
	}

	return weightedProgress / totalWeight
}

// calculateTaskWeight calculates the weight of a task based on its characteristics
func (pt *ProgressTracker) calculateTaskWeight(node *DAGNode) float64 {
	weight := 1.0

	// Critical path tasks have higher weight
	if node.CriticalPath {
		weight *= 1.5
	}

	// Tasks with more dependents have higher weight
	if len(node.Dependents) > 0 {
		weight *= (1.0 + float64(len(node.Dependents))*0.1)
	}

	// Tasks with longer timeout have higher weight
	if node.Task.Timeout > 60 {
		weight *= (1.0 + float64(node.Task.Timeout)/300.0*0.2)
	}

	// Different task types have different base weights
	switch node.Task.Type {
	case TaskTypeCommand:
		weight *= 1.2
	case TaskTypeValidation:
		weight *= 1.0
	case TaskTypeFile:
		weight *= 0.8
	case TaskTypeHTTP:
		weight *= 1.1
	case TaskTypeManual:
		weight *= 2.0 // Manual tasks are typically more significant
	case TaskTypeConditional:
		weight *= 0.9
	}

	return weight
}

// estimateCompletionTime estimates when the workflow will complete
func (pt *ProgressTracker) estimateCompletionTime(workflowExecution *WorkflowExecution, taskExecutions map[string]*TaskExecution, overallProgress float64) *time.Time {
	if overallProgress >= 100.0 {
		return nil // Already complete
	}

	if workflowExecution.StartedAt == nil {
		return nil // Not started yet
	}

	elapsed := time.Since(*workflowExecution.StartedAt)
	
	// Avoid division by zero
	if overallProgress <= 0 {
		return nil
	}

	// Simple linear estimation: remaining_time = elapsed_time * (100 - progress) / progress
	remainingTime := time.Duration(float64(elapsed) * (100.0 - overallProgress) / overallProgress)
	
	// Add buffer for uncertainty (20%)
	remainingTime = time.Duration(float64(remainingTime) * 1.2)

	estimatedCompletion := time.Now().Add(remainingTime)
	return &estimatedCompletion
}

// TrackTaskDependencyProgress tracks progress considering task dependencies
func (pt *ProgressTracker) TrackTaskDependencyProgress(taskID string, taskExecutions map[string]*TaskExecution) *TaskDependencyProgress {
	node, exists := pt.dependencyManager.nodes[taskID]
	if !exists {
		return nil
	}

	// Calculate dependency progress
	dependencyProgress := make(map[string]float64)
	completedDependencies := 0
	totalDependencies := len(node.Task.Dependencies)

	for _, depID := range node.Task.Dependencies {
		progress := pt.calculateTaskProgress(depID, taskExecutions)
		dependencyProgress[depID] = progress
		
		if progress >= 100.0 {
			completedDependencies++
		}
	}

	// Calculate dependency completion percentage
	var dependencyCompletion float64
	if totalDependencies > 0 {
		dependencyCompletion = float64(completedDependencies) / float64(totalDependencies) * 100.0
	} else {
		dependencyCompletion = 100.0 // No dependencies means 100% dependency completion
	}

	// Check if task is ready to run
	isReady := pt.isTaskReady(taskID, taskExecutions)

	// Calculate dependent task progress
	dependentTasks := pt.dependencyManager.GetDependentTasks(taskID)
	dependentProgress := make(map[string]float64)
	for _, depTask := range dependentTasks {
		dependentProgress[depTask.ID] = pt.calculateTaskProgress(depTask.ID, taskExecutions)
	}

	return &TaskDependencyProgress{
		TaskID:                taskID,
		TaskProgress:          pt.calculateTaskProgress(taskID, taskExecutions),
		DependencyProgress:    dependencyProgress,
		DependencyCompletion:  dependencyCompletion,
		IsReady:               isReady,
		DependentProgress:     dependentProgress,
		LastUpdated:           time.Now(),
	}
}

// isTaskReady checks if a task is ready to run based on dependencies
func (pt *ProgressTracker) isTaskReady(taskID string, taskExecutions map[string]*TaskExecution) bool {
	node, exists := pt.dependencyManager.nodes[taskID]
	if !exists {
		return false
	}

	// Check if task is already running or completed
	if execution, hasExecution := taskExecutions[taskID]; hasExecution {
		if execution.Status == TaskStatusRunning || execution.Status == TaskStatusCompleted || execution.Status == TaskStatusFailed {
			return false
		}
	}

	// Check all dependencies
	for _, depID := range node.Task.Dependencies {
		if execution, hasExecution := taskExecutions[depID]; hasExecution {
			if execution.Status != TaskStatusCompleted {
				return false
			}
		} else {
			return false // Dependency not started
		}
	}

	return true
}

// CalculateParallelismMetrics calculates parallelism utilization metrics
func (pt *ProgressTracker) CalculateParallelismMetrics(taskExecutions map[string]*TaskExecution) *ParallelismMetrics {
	totalTasks := len(pt.dependencyManager.nodes)
	runningTasks := 0
	readyTasks := 0
	
	runnableTasks := pt.dependencyManager.GetRunnableTasks(taskExecutions)
	readyTasks = len(runnableTasks)

	for _, execution := range taskExecutions {
		if execution.Status == TaskStatusRunning {
			runningTasks++
		}
	}

	// Calculate theoretical maximum parallelism (tasks that could run if resources were unlimited)
	theoreticalMaxParallelism := readyTasks + runningTasks

	// Calculate current utilization
	var utilizationPercent float64
	if theoreticalMaxParallelism > 0 {
		utilizationPercent = float64(runningTasks) / float64(theoreticalMaxParallelism) * 100.0
	}

	return &ParallelismMetrics{
		TotalTasks:                 totalTasks,
		RunningTasks:               runningTasks,
		ReadyTasks:                 readyTasks,
		TheoreticalMaxParallelism:  theoreticalMaxParallelism,
		CurrentUtilizationPercent:  utilizationPercent,
		LastCalculated:             time.Now(),
	}
}

// Support structures

// TaskDependencyProgress represents progress tracking for task dependencies
type TaskDependencyProgress struct {
	TaskID                string             `json:"task_id"`
	TaskProgress          float64            `json:"task_progress"`
	DependencyProgress    map[string]float64 `json:"dependency_progress"`
	DependencyCompletion  float64            `json:"dependency_completion"`
	IsReady               bool               `json:"is_ready"`
	DependentProgress     map[string]float64 `json:"dependent_progress"`
	LastUpdated           time.Time          `json:"last_updated"`
}

// ParallelismMetrics represents parallelism utilization metrics
type ParallelismMetrics struct {
	TotalTasks                 int       `json:"total_tasks"`
	RunningTasks               int       `json:"running_tasks"`
	ReadyTasks                 int       `json:"ready_tasks"`
	TheoreticalMaxParallelism  int       `json:"theoretical_max_parallelism"`
	CurrentUtilizationPercent  float64   `json:"current_utilization_percent"`
	LastCalculated             time.Time `json:"last_calculated"`
}

// ProgressSnapshot represents a point-in-time snapshot of workflow progress
type ProgressSnapshot struct {
	WorkflowExecutionID string                             `json:"workflow_execution_id"`
	Timestamp           time.Time                          `json:"timestamp"`
	OverallProgress     float64                            `json:"overall_progress"`
	TaskProgresses      map[string]float64                 `json:"task_progresses"`
	TaskDependencies    map[string]*TaskDependencyProgress `json:"task_dependencies"`
	ParallelismMetrics  *ParallelismMetrics                `json:"parallelism_metrics"`
	CriticalPath        []string                           `json:"critical_path"`
	EstimatedCompletion *time.Time                         `json:"estimated_completion"`
}

// CreateProgressSnapshot creates a comprehensive progress snapshot
func (pt *ProgressTracker) CreateProgressSnapshot(workflowExecution *WorkflowExecution, taskExecutions map[string]*TaskExecution, analysis *DAGAnalysis) *ProgressSnapshot {
	progress := pt.CalculateDetailedProgress(workflowExecution, taskExecutions)
	parallelismMetrics := pt.CalculateParallelismMetrics(taskExecutions)

	taskDependencies := make(map[string]*TaskDependencyProgress)
	for taskID := range pt.dependencyManager.nodes {
		taskDependencies[taskID] = pt.TrackTaskDependencyProgress(taskID, taskExecutions)
	}

	return &ProgressSnapshot{
		WorkflowExecutionID: workflowExecution.ID,
		Timestamp:           time.Now(),
		OverallProgress:     progress.OverallProgress,
		TaskProgresses:      progress.TaskProgress,
		TaskDependencies:    taskDependencies,
		ParallelismMetrics:  parallelismMetrics,
		CriticalPath:        analysis.CriticalPath,
		EstimatedCompletion: progress.EstimatedCompletion,
	}
}

// ProgressHistory manages historical progress data
type ProgressHistory struct {
	snapshots []ProgressSnapshot
	maxSize   int
}

// NewProgressHistory creates a new progress history tracker
func NewProgressHistory(maxSize int) *ProgressHistory {
	return &ProgressHistory{
		snapshots: make([]ProgressSnapshot, 0, maxSize),
		maxSize:   maxSize,
	}
}

// AddSnapshot adds a new progress snapshot
func (ph *ProgressHistory) AddSnapshot(snapshot ProgressSnapshot) {
	ph.snapshots = append(ph.snapshots, snapshot)
	
	// Remove oldest snapshots if we exceed max size
	if len(ph.snapshots) > ph.maxSize {
		ph.snapshots = ph.snapshots[1:]
	}
}

// GetSnapshots returns all snapshots
func (ph *ProgressHistory) GetSnapshots() []ProgressSnapshot {
	return ph.snapshots
}

// GetLatestSnapshot returns the most recent snapshot
func (ph *ProgressHistory) GetLatestSnapshot() *ProgressSnapshot {
	if len(ph.snapshots) == 0 {
		return nil
	}
	return &ph.snapshots[len(ph.snapshots)-1]
}

// CalculateProgressVelocity calculates the rate of progress change
func (ph *ProgressHistory) CalculateProgressVelocity() float64 {
	if len(ph.snapshots) < 2 {
		return 0.0
	}

	latest := ph.snapshots[len(ph.snapshots)-1]
	previous := ph.snapshots[len(ph.snapshots)-2]

	timeDiff := latest.Timestamp.Sub(previous.Timestamp)
	if timeDiff <= 0 {
		return 0.0
	}

	progressDiff := latest.OverallProgress - previous.OverallProgress
	
	// Return progress per hour
	return progressDiff / timeDiff.Hours()
}