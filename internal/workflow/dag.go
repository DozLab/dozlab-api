package workflow

import (
	"fmt"
	"sort"
	"time"
)

// DAGManager handles DAG analysis and validation
type DAGManager struct{}

// NewDAGManager creates a new DAG manager
func NewDAGManager() *DAGManager {
	return &DAGManager{}
}

// BuildDAG constructs a DAG from workflow tasks
func (dm *DAGManager) BuildDAG(workflow *Workflow) (*DAGAnalysis, map[string]*DAGNode, error) {
	if len(workflow.Tasks) == 0 {
		return &DAGAnalysis{
			IsValid:        true,
			HasCycles:      false,
			MaxDepth:       0,
			ParallelLevels: [][]string{},
		}, make(map[string]*DAGNode), nil
	}

	// Create nodes map
	nodes := make(map[string]*DAGNode)
	for _, task := range workflow.Tasks {
		nodes[task.ID] = &DAGNode{
			ID:           task.ID,
			Task:         &task,
			Dependencies: make([]*DAGNode, 0),
			Dependents:   make([]*DAGNode, 0),
		}
	}

	// Build dependency relationships
	for _, task := range workflow.Tasks {
		node := nodes[task.ID]
		for _, depID := range task.Dependencies {
			if depNode, exists := nodes[depID]; exists {
				node.Dependencies = append(node.Dependencies, depNode)
				depNode.Dependents = append(depNode.Dependents, node)
			} else {
				return nil, nil, fmt.Errorf("task %s has unknown dependency %s", task.ID, depID)
			}
		}
	}

	// Analyze the DAG
	analysis, err := dm.analyzeDAG(nodes)
	if err != nil {
		return nil, nil, err
	}

	return analysis, nodes, nil
}

// analyzeDAG performs comprehensive DAG analysis
func (dm *DAGManager) analyzeDAG(nodes map[string]*DAGNode) (*DAGAnalysis, error) {
	analysis := &DAGAnalysis{
		IsValid:        true,
		HasCycles:      false,
		Cycles:         make([][]string, 0),
		CriticalPath:   make([]string, 0),
		ParallelLevels: make([][]string, 0),
	}

	// Check for cycles using DFS
	if cycles := dm.detectCycles(nodes); len(cycles) > 0 {
		analysis.IsValid = false
		analysis.HasCycles = true
		analysis.Cycles = cycles
		return analysis, fmt.Errorf("workflow contains cycles: %v", cycles)
	}

	// Calculate topological levels
	levels := dm.calculateTopologicalLevels(nodes)
	analysis.ParallelLevels = levels
	analysis.MaxDepth = int32(len(levels))

	// Calculate critical path
	criticalPath, estimatedTime := dm.calculateCriticalPath(nodes)
	analysis.CriticalPath = criticalPath
	analysis.EstimatedTime = estimatedTime

	// Mark critical path nodes
	criticalPathSet := make(map[string]bool)
	for _, taskID := range criticalPath {
		criticalPathSet[taskID] = true
	}
	for _, node := range nodes {
		node.CriticalPath = criticalPathSet[node.ID]
		node.Level = dm.getNodeLevel(node, levels)
	}

	return analysis, nil
}

// detectCycles uses DFS to detect cycles in the DAG
func (dm *DAGManager) detectCycles(nodes map[string]*DAGNode) [][]string {
	var cycles [][]string
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	path := make([]string, 0)

	var dfs func(node *DAGNode) bool
	dfs = func(node *DAGNode) bool {
		visited[node.ID] = true
		recStack[node.ID] = true
		path = append(path, node.ID)

		for _, dep := range node.Dependencies {
			if !visited[dep.ID] {
				if dfs(dep) {
					return true
				}
			} else if recStack[dep.ID] {
				// Found cycle - extract cycle from path
				cycleStart := -1
				for i, id := range path {
					if id == dep.ID {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cycle := make([]string, len(path)-cycleStart)
					copy(cycle, path[cycleStart:])
					cycles = append(cycles, cycle)
				}
				return true
			}
		}

		recStack[node.ID] = false
		if len(path) > 0 {
			path = path[:len(path)-1]
		}
		return false
	}

	for _, node := range nodes {
		if !visited[node.ID] {
			path = path[:0] // Reset path for new DFS tree
			dfs(node)
		}
	}

	return cycles
}

// calculateTopologicalLevels calculates parallel execution levels
func (dm *DAGManager) calculateTopologicalLevels(nodes map[string]*DAGNode) [][]string {
	// Calculate in-degree for each node
	inDegree := make(map[string]int)
	for _, node := range nodes {
		inDegree[node.ID] = len(node.Dependencies)
	}

	levels := make([][]string, 0)
	remaining := make(map[string]*DAGNode)
	for id, node := range nodes {
		remaining[id] = node
	}

	for len(remaining) > 0 {
		// Find all nodes with in-degree 0
		currentLevel := make([]string, 0)
		for id, degree := range inDegree {
			if degree == 0 && remaining[id] != nil {
				currentLevel = append(currentLevel, id)
			}
		}

		if len(currentLevel) == 0 {
			// This shouldn't happen if there are no cycles
			break
		}

		// Sort for consistent ordering
		sort.Strings(currentLevel)
		levels = append(levels, currentLevel)

		// Remove current level nodes and update in-degrees
		for _, id := range currentLevel {
			node := remaining[id]
			delete(remaining, id)
			delete(inDegree, id)

			// Decrease in-degree of dependent nodes
			for _, dependent := range node.Dependents {
				if inDegree[dependent.ID] > 0 {
					inDegree[dependent.ID]--
				}
			}
		}
	}

	return levels
}

// calculateCriticalPath finds the critical path through the DAG
func (dm *DAGManager) calculateCriticalPath(nodes map[string]*DAGNode) ([]string, int32) {
	// Calculate earliest start time for each node
	earliestStart := make(map[string]int32)
	visited := make(map[string]bool)

	var calculateEarliestStart func(node *DAGNode) int32
	calculateEarliestStart = func(node *DAGNode) int32 {
		if visited[node.ID] {
			return earliestStart[node.ID]
		}

		visited[node.ID] = true
		maxDepTime := int32(0)

		for _, dep := range node.Dependencies {
			depTime := calculateEarliestStart(dep) + dep.Task.Timeout
			if depTime > maxDepTime {
				maxDepTime = depTime
			}
		}

		earliestStart[node.ID] = maxDepTime
		return maxDepTime
	}

	// Calculate earliest start for all nodes
	for _, node := range nodes {
		calculateEarliestStart(node)
	}

	// Find the critical path by backtracking from the latest finishing node
	latestFinish := int32(0)
	var endNode *DAGNode

	for _, node := range nodes {
		finish := earliestStart[node.ID] + node.Task.Timeout
		if finish > latestFinish {
			latestFinish = finish
			endNode = node
		}
	}

	// Backtrack to find critical path
	criticalPath := make([]string, 0)
	if endNode != nil {
		visited = make(map[string]bool)
		dm.backtrackCriticalPath(endNode, earliestStart, &criticalPath, visited)
		
		// Reverse to get forward order
		for i, j := 0, len(criticalPath)-1; i < j; i, j = i+1, j-1 {
			criticalPath[i], criticalPath[j] = criticalPath[j], criticalPath[i]
		}
	}

	return criticalPath, latestFinish
}

// backtrackCriticalPath recursively finds the critical path
func (dm *DAGManager) backtrackCriticalPath(node *DAGNode, earliestStart map[string]int32, path *[]string, visited map[string]bool) {
	if visited[node.ID] {
		return
	}

	visited[node.ID] = true
	*path = append(*path, node.ID)

	// Find the dependency that determines this node's earliest start time
	nodeEarliestStart := earliestStart[node.ID]
	for _, dep := range node.Dependencies {
		depFinish := earliestStart[dep.ID] + dep.Task.Timeout
		if depFinish == nodeEarliestStart {
			dm.backtrackCriticalPath(dep, earliestStart, path, visited)
			break // Only follow one critical dependency
		}
	}
}

// getNodeLevel finds the level of a node in the topological ordering
func (dm *DAGManager) getNodeLevel(node *DAGNode, levels [][]string) int32 {
	for i, level := range levels {
		for _, id := range level {
			if id == node.ID {
				return int32(i)
			}
		}
	}
	return -1 // Should not happen
}

// TaskDependencyManager manages task dependencies during execution
type TaskDependencyManager struct {
	nodes map[string]*DAGNode
}

// NewTaskDependencyManager creates a new task dependency manager
func NewTaskDependencyManager(nodes map[string]*DAGNode) *TaskDependencyManager {
	return &TaskDependencyManager{
		nodes: nodes,
	}
}

// GetRunnableTasks returns tasks that can be executed based on current state
func (tdm *TaskDependencyManager) GetRunnableTasks(taskExecutions map[string]*TaskExecution) []*Task {
	runnableTasks := make([]*Task, 0)
	
	// Build completed tasks map
	completedTasks := make(map[string]bool)
	for taskID, execution := range taskExecutions {
		if execution.Status == TaskStatusCompleted {
			completedTasks[taskID] = true
		}
	}

	for _, node := range tdm.nodes {
		execution, hasExecution := taskExecutions[node.ID]
		
		// Skip if task is already running or completed
		if hasExecution && (execution.Status == TaskStatusRunning || 
							execution.Status == TaskStatusCompleted || 
							execution.Status == TaskStatusFailed) {
			continue
		}

		// Check if all dependencies are completed
		if node.Task.IsRunnable(completedTasks) {
			runnableTasks = append(runnableTasks, node.Task)
		}
	}

	return runnableTasks
}

// GetBlockedTasks returns tasks that are blocked by failed dependencies
func (tdm *TaskDependencyManager) GetBlockedTasks(taskExecutions map[string]*TaskExecution) []*Task {
	blockedTasks := make([]*Task, 0)
	
	// Build failed tasks map
	failedTasks := make(map[string]bool)
	for taskID, execution := range taskExecutions {
		if execution.Status == TaskStatusFailed {
			failedTasks[taskID] = true
		}
	}

	for _, node := range tdm.nodes {
		execution, hasExecution := taskExecutions[node.ID]
		
		// Skip if task is already completed or failed
		if hasExecution && (execution.Status == TaskStatusCompleted || 
							execution.Status == TaskStatusFailed) {
			continue
		}

		// Check if any dependency has failed
		isBlocked := false
		for _, depID := range node.Task.Dependencies {
			if failedTasks[depID] {
				isBlocked = true
				break
			}
		}

		if isBlocked {
			blockedTasks = append(blockedTasks, node.Task)
		}
	}

	return blockedTasks
}

// GetDependentTasks returns tasks that depend on the given task
func (tdm *TaskDependencyManager) GetDependentTasks(taskID string) []*Task {
	node, exists := tdm.nodes[taskID]
	if !exists {
		return []*Task{}
	}

	dependentTasks := make([]*Task, 0, len(node.Dependents))
	for _, dependent := range node.Dependents {
		dependentTasks = append(dependentTasks, dependent.Task)
	}

	return dependentTasks
}

// ValidateTaskExecution validates if a task can be executed based on dependencies
func (tdm *TaskDependencyManager) ValidateTaskExecution(taskID string, taskExecutions map[string]*TaskExecution) error {
	node, exists := tdm.nodes[taskID]
	if !exists {
		return fmt.Errorf("task %s not found in workflow", taskID)
	}

	// Check if task is already running or completed
	if execution, hasExecution := taskExecutions[taskID]; hasExecution {
		if execution.Status == TaskStatusRunning {
			return fmt.Errorf("task %s is already running", taskID)
		}
		if execution.Status == TaskStatusCompleted {
			return fmt.Errorf("task %s is already completed", taskID)
		}
	}

	// Build completed tasks map
	completedTasks := make(map[string]bool)
	for execTaskID, execution := range taskExecutions {
		if execution.Status == TaskStatusCompleted {
			completedTasks[execTaskID] = true
		}
	}

	// Check dependencies
	for _, depID := range node.Task.Dependencies {
		if !completedTasks[depID] {
			if depExecution, hasDepExecution := taskExecutions[depID]; hasDepExecution {
				if depExecution.Status == TaskStatusFailed {
					return fmt.Errorf("task %s cannot run because dependency %s failed", taskID, depID)
				}
			}
			return fmt.Errorf("task %s cannot run because dependency %s is not completed", taskID, depID)
		}
	}

	return nil
}

// CalculateProgress calculates the overall workflow progress
func (tdm *TaskDependencyManager) CalculateProgress(taskExecutions map[string]*TaskExecution) *WorkflowProgress {
	totalTasks := int32(len(tdm.nodes))
	completedTasks := int32(0)
	failedTasks := int32(0)
	currentTasks := make([]string, 0)
	taskProgress := make(map[string]float64)

	for taskID := range tdm.nodes {
		if execution, hasExecution := taskExecutions[taskID]; hasExecution {
			switch execution.Status {
			case TaskStatusCompleted:
				completedTasks++
				taskProgress[taskID] = 100.0
			case TaskStatusFailed:
				failedTasks++
				taskProgress[taskID] = 0.0
			case TaskStatusRunning:
				currentTasks = append(currentTasks, taskID)
				taskProgress[taskID] = 50.0 // Assume 50% progress for running tasks
			default:
				taskProgress[taskID] = 0.0
			}
		} else {
			taskProgress[taskID] = 0.0
		}
	}

	overallProgress := float64(completedTasks) / float64(totalTasks) * 100.0

	return &WorkflowProgress{
		OverallProgress: overallProgress,
		CompletedTasks:  completedTasks,
		TotalTasks:      totalTasks,
		FailedTasks:     failedTasks,
		CurrentTasks:    currentTasks,
		TaskProgress:    taskProgress,
		LastUpdated:     time.Now(),
	}
}