package examiner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dozlab-backend/internal/websocket"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ValidationEngine handles automatic solution checking
type ValidationEngine struct {
	db       *gorm.DB
	eventBus websocket.EventPublisher
}

// NewValidationEngine creates a new validation engine
func NewValidationEngine(db *gorm.DB, eventBus websocket.EventPublisher) *ValidationEngine {
	return &ValidationEngine{
		db:       db,
		eventBus: eventBus,
	}
}

// ValidateTask validates all rules for a specific task
func (ve *ValidationEngine) ValidateTask(ctx context.Context, sessionID, userID, labID, taskID, workspacePath string) (*TaskValidation, error) {
	// Get all validation rules for the task
	var rules []ValidationRule
	if err := ve.db.Where("lab_id = ? AND task_id = ? AND enabled = ?", labID, taskID, true).
		Order("\"order\" ASC").Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("failed to get validation rules: %w", err)
	}

	if len(rules) == 0 {
		return ve.createEmptyTaskValidation(sessionID, userID, labID, taskID), nil
	}

	// Create or get existing task validation
	taskValidation, err := ve.getOrCreateTaskValidation(sessionID, userID, labID, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to create task validation: %w", err)
	}

	taskValidation.Status = TaskValidationStatusRunning
	taskValidation.TotalRules = int32(len(rules))
	taskValidation.LastAttemptAt = time.Now()
	taskValidation.Attempts++

	if err := ve.db.Save(taskValidation).Error; err != nil {
		return nil, fmt.Errorf("failed to update task validation: %w", err)
	}

	// Execute validation rules
	var totalScore float64
	var maxScore float64
	var passedRules, failedRules, skippedRules int32

	for _, rule := range rules {
		result, err := ve.executeRule(ctx, &rule, sessionID, userID, workspacePath)
		if err != nil {
			// Log error but continue with other rules
			ve.logValidationError(ctx, sessionID, userID, rule.ID, err)
			skippedRules++
			continue
		}

		// Save validation result
		if err := ve.db.Create(result).Error; err != nil {
			ve.logValidationError(ctx, sessionID, userID, rule.ID, err)
		}

		// Update counters and scores
		maxScore += rule.Weight
		if result.Passed {
			totalScore += result.Score
			passedRules++
		} else {
			failedRules++
		}

		// Publish real-time validation result
		ve.publishValidationResult(ctx, result)
	}

	// Update task validation with final results
	taskValidation.PassedRules = passedRules
	taskValidation.FailedRules = failedRules
	taskValidation.SkippedRules = skippedRules
	taskValidation.Score = totalScore
	taskValidation.MaxScore = maxScore
	if maxScore > 0 {
		taskValidation.Percentage = (totalScore / maxScore) * 100
	}

	// Determine completion status
	if failedRules == 0 && skippedRules == 0 {
		taskValidation.Status = TaskValidationStatusCompleted
	} else if passedRules > 0 {
		taskValidation.Status = TaskValidationStatusPartial
	} else {
		taskValidation.Status = TaskValidationStatusFailed
	}

	// Generate feedback
	taskValidation.Feedback = ve.generateTaskFeedback(taskValidation, rules)
	taskValidation.CompletionTime = int32(time.Since(taskValidation.LastAttemptAt).Seconds())

	if err := ve.db.Save(taskValidation).Error; err != nil {
		return nil, fmt.Errorf("failed to save task validation: %w", err)
	}

	// Publish task completion event
	ve.publishTaskValidationComplete(ctx, taskValidation)

	return taskValidation, nil
}

// executeRule executes a single validation rule
func (ve *ValidationEngine) executeRule(ctx context.Context, rule *ValidationRule, sessionID, userID, workspacePath string) (*ValidationResult, error) {
	result := &ValidationResult{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		UserID:    userID,
		LabID:     rule.LabID,
		TaskID:    rule.TaskID,
		RuleID:    rule.ID,
		Status:    ValidationStatusRunning,
		MaxScore:  rule.Weight,
		StartedAt: time.Now(),
	}

	// Create context with timeout
	timeout := time.Duration(rule.Timeout) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	// Execute validation based on type
	var err error
	switch rule.Type {
	case ValidationTypeFileExists:
		err = ve.validateFileExists(execCtx, rule, result, workspacePath)
	case ValidationTypeFileContent:
		err = ve.validateFileContent(execCtx, rule, result, workspacePath)
	case ValidationTypeFileSize:
		err = ve.validateFileSize(execCtx, rule, result, workspacePath)
	case ValidationTypeFilePermissions:
		err = ve.validateFilePermissions(execCtx, rule, result, workspacePath)
	case ValidationTypeDirectoryExists:
		err = ve.validateDirectoryExists(execCtx, rule, result, workspacePath)
	case ValidationTypeCommandOutput:
		err = ve.validateCommandOutput(execCtx, rule, result, workspacePath)
	case ValidationTypeCommandExitCode:
		err = ve.validateCommandExitCode(execCtx, rule, result, workspacePath)
	case ValidationTypeProcessRunning:
		err = ve.validateProcessRunning(execCtx, rule, result, workspacePath)
	case ValidationTypePortOpen:
		err = ve.validatePortOpen(execCtx, rule, result)
	case ValidationTypeRegexMatch:
		err = ve.validateRegexMatch(execCtx, rule, result, workspacePath)
	case ValidationTypeHTTPResponse:
		err = ve.validateHTTPResponse(execCtx, rule, result)
	case ValidationTypeHTTPStatusCode:
		err = ve.validateHTTPStatusCode(execCtx, rule, result)
	case ValidationTypeCustomScript:
		err = ve.validateCustomScript(execCtx, rule, result, workspacePath)
	default:
		err = fmt.Errorf("unsupported validation type: %s", rule.Type)
	}

	// Calculate execution time
	executionTime := time.Since(startTime)
	result.ExecutionTime = int32(executionTime.Milliseconds())

	// Handle timeout
	if execCtx.Err() == context.DeadlineExceeded {
		result.Status = ValidationStatusTimeout
		result.Message = "Validation timed out"
		result.Passed = false
		result.Score = 0
	} else if err != nil {
		result.Status = ValidationStatusFailed
		result.Message = err.Error()
		result.Passed = false
		result.Score = 0
		result.Details.Error = err.Error()
	} else {
		result.Status = ValidationStatusCompleted
		if result.Passed {
			result.Score = rule.Weight
		}
	}

	now := time.Now()
	result.CompletedAt = &now

	return result, nil
}

// File validation methods

func (ve *ValidationEngine) validateFileExists(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	filePath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	info, err := os.Stat(filePath)
	exists := err == nil

	result.Details.FileInfo = &FileInfo{
		Exists: exists,
	}

	if exists {
		result.Details.FileInfo.Size = info.Size()
		result.Details.FileInfo.IsDir = info.IsDir()
		result.Details.FileInfo.Permissions = info.Mode().Perm().String()
		result.Details.FileInfo.ModTime = info.ModTime().Format(time.RFC3339)
	}

	result.Passed = exists
	if exists {
		result.Message = fmt.Sprintf("File exists: %s", rule.Condition.FilePath)
	} else {
		result.Message = fmt.Sprintf("File does not exist: %s", rule.Condition.FilePath)
	}

	return nil
}

func (ve *ValidationEngine) validateFileContent(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	filePath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	content, err := os.ReadFile(filePath)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Cannot read file: %s", err.Error())
		return nil
	}

	contentStr := string(content)
	result.Details.FileInfo = &FileInfo{
		Exists:  true,
		Content: contentStr,
		Size:    int64(len(content)),
	}

	// Check content based on pattern or expected value
	if rule.Condition.Pattern != "" {
		matched, err := regexp.MatchString(rule.Condition.Pattern, contentStr)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		result.Passed = matched
		result.Message = fmt.Sprintf("Pattern match: %t", matched)
	} else if rule.Condition.Expected != "" {
		if rule.Condition.CaseSensitive {
			result.Passed = strings.Contains(contentStr, rule.Condition.Expected)
		} else {
			result.Passed = strings.Contains(strings.ToLower(contentStr), strings.ToLower(rule.Condition.Expected))
		}
		result.Message = fmt.Sprintf("Content contains expected text: %t", result.Passed)
	}

	return nil
}

func (ve *ValidationEngine) validateFileSize(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	filePath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	info, err := os.Stat(filePath)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Cannot access file: %s", err.Error())
		return nil
	}

	size := info.Size()
	result.Details.FileInfo = &FileInfo{
		Exists: true,
		Size:   size,
	}

	// Check size constraints
	passed := true
	if rule.Condition.MinSize > 0 && size < rule.Condition.MinSize {
		passed = false
	}
	if rule.Condition.MaxSize > 0 && size > rule.Condition.MaxSize {
		passed = false
	}

	result.Passed = passed
	result.Message = fmt.Sprintf("File size %d bytes within constraints: %t", size, passed)
	return nil
}

func (ve *ValidationEngine) validateFilePermissions(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	filePath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	info, err := os.Stat(filePath)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Cannot access file: %s", err.Error())
		return nil
	}

	actualPerms := info.Mode().Perm().String()
	expectedPerms := rule.Condition.Permissions

	result.Details.FileInfo = &FileInfo{
		Exists:      true,
		Permissions: actualPerms,
	}

	result.Passed = actualPerms == expectedPerms
	result.Message = fmt.Sprintf("File permissions %s match expected %s: %t", actualPerms, expectedPerms, result.Passed)
	return nil
}

func (ve *ValidationEngine) validateDirectoryExists(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	dirPath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	info, err := os.Stat(dirPath)
	exists := err == nil && info.IsDir()

	result.Details.FileInfo = &FileInfo{
		Exists: exists,
		IsDir:  exists,
	}

	result.Passed = exists
	if exists {
		result.Message = fmt.Sprintf("Directory exists: %s", rule.Condition.FilePath)
	} else {
		result.Message = fmt.Sprintf("Directory does not exist: %s", rule.Condition.FilePath)
	}

	return nil
}

// Command validation methods

func (ve *ValidationEngine) validateCommandOutput(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	// Validate command before execution
	if err := ve.validateCommand(rule.Condition.Command, rule.Condition.Arguments); err != nil {
		return fmt.Errorf("command validation failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, rule.Condition.Command, rule.Condition.Arguments...)
	if rule.Condition.WorkingDir != "" {
		workingDir := filepath.Join(workspacePath, rule.Condition.WorkingDir)
		// Ensure working directory is within workspace bounds
		if !strings.HasPrefix(workingDir, workspacePath) {
			return fmt.Errorf("working directory outside workspace bounds")
		}
		cmd.Dir = workingDir
	} else {
		cmd.Dir = workspacePath
	}

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	outputStr := string(output)
	result.Details.CommandOutput = &CommandOutput{
		Command:  rule.Condition.Command,
		ExitCode: cmd.ProcessState.ExitCode(),
		Stdout:   outputStr,
		Duration: duration.Milliseconds(),
		TimedOut: ctx.Err() == context.DeadlineExceeded,
	}

	if err != nil {
		result.Details.CommandOutput.Stderr = err.Error()
	}

	// Check output against expected pattern or value
	if rule.Condition.Pattern != "" {
		matched, regexErr := regexp.MatchString(rule.Condition.Pattern, outputStr)
		if regexErr != nil {
			return fmt.Errorf("invalid regex pattern: %w", regexErr)
		}
		result.Passed = matched
		result.Message = fmt.Sprintf("Command output matches pattern: %t", matched)
	} else if rule.Condition.Expected != "" {
		if rule.Condition.CaseSensitive {
			result.Passed = strings.Contains(outputStr, rule.Condition.Expected)
		} else {
			result.Passed = strings.Contains(strings.ToLower(outputStr), strings.ToLower(rule.Condition.Expected))
		}
		result.Message = fmt.Sprintf("Command output contains expected text: %t", result.Passed)
	} else {
		result.Passed = err == nil
		result.Message = fmt.Sprintf("Command executed successfully: %t", result.Passed)
	}

	return nil
}

func (ve *ValidationEngine) validateCommandExitCode(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	// Validate command before execution
	if err := ve.validateCommand(rule.Condition.Command, rule.Condition.Arguments); err != nil {
		return fmt.Errorf("command validation failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, rule.Condition.Command, rule.Condition.Arguments...)
	if rule.Condition.WorkingDir != "" {
		workingDir := filepath.Join(workspacePath, rule.Condition.WorkingDir)
		// Ensure working directory is within workspace bounds
		if !strings.HasPrefix(workingDir, workspacePath) {
			return fmt.Errorf("working directory outside workspace bounds")
		}
		cmd.Dir = workingDir
	} else {
		cmd.Dir = workspacePath
	}

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	actualExitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			actualExitCode = exitError.ExitCode()
		}
	}

	result.Details.CommandOutput = &CommandOutput{
		Command:  rule.Condition.Command,
		ExitCode: actualExitCode,
		Duration: duration.Milliseconds(),
		TimedOut: ctx.Err() == context.DeadlineExceeded,
	}

	expectedExitCode := rule.Condition.ExitCode
	result.Passed = actualExitCode == expectedExitCode
	result.Message = fmt.Sprintf("Command exit code %d matches expected %d: %t", actualExitCode, expectedExitCode, result.Passed)
	return nil
}

func (ve *ValidationEngine) validateProcessRunning(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	// Use ps command to check for running process
	cmd := exec.CommandContext(ctx, "ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get process list: %w", err)
	}

	outputStr := string(output)
	processName := rule.Condition.ProcessName
	isRunning := strings.Contains(outputStr, processName)

	result.Details.CommandOutput = &CommandOutput{
		Command:  "ps aux",
		ExitCode: cmd.ProcessState.ExitCode(),
		Stdout:   outputStr,
	}

	result.Passed = isRunning
	result.Message = fmt.Sprintf("Process '%s' is running: %t", processName, isRunning)
	return nil
}

func (ve *ValidationEngine) validatePortOpen(ctx context.Context, rule *ValidationRule, result *ValidationResult) error {
	port := rule.Condition.Port
	address := fmt.Sprintf("localhost:%d", port)

	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	isOpen := err == nil
	if conn != nil {
		conn.Close()
	}

	result.Passed = isOpen
	result.Message = fmt.Sprintf("Port %d is open: %t", port, isOpen)
	return nil
}

// validateCommand validates that a command is safe to execute
func (ve *ValidationEngine) validateCommand(command string, args []string) error {
	// Allowlist of safe commands
	allowedCommands := map[string]bool{
		"echo":    true,
		"cat":     true,
		"ls":      true,
		"pwd":     true,
		"whoami":  true,
		"date":    true,
		"grep":    true,
		"find":    true,
		"head":    true,
		"tail":    true,
		"wc":      true,
		"sort":    true,
		"uniq":    true,
		"cut":     true,
		"tr":      true,
		"sed":     true,
		"awk":     true,
		"python3": true,
		"python":  true,
		"node":    true,
		"npm":     true,
		"go":      true,
		"git":     true,
		"docker":  true,
		"kubectl": true,
		"ps":      true,
		"netstat": true,
		"curl":    true,
		"wget":    true,
	}

	// Check if command is in allowlist
	if !allowedCommands[command] {
		return fmt.Errorf("command '%s' is not allowed", command)
	}

	// Additional validation for specific commands
	switch command {
	case "rm", "rmdir", "del", "delete":
		return fmt.Errorf("destructive command '%s' is not allowed", command)
	case "chmod", "chown", "su", "sudo":
		return fmt.Errorf("privilege escalation command '%s' is not allowed", command)
	case "curl", "wget":
		// Restrict network access to safe domains
		for _, arg := range args {
			if strings.Contains(arg, "://") {
				if !ve.isAllowedURL(arg) {
					return fmt.Errorf("URL '%s' is not allowed", arg)
				}
			}
		}
	}

	// Check for dangerous argument patterns
	for _, arg := range args {
		// Prevent command injection
		if strings.Contains(arg, ";") || strings.Contains(arg, "&&") || 
		   strings.Contains(arg, "||") || strings.Contains(arg, "|") ||
		   strings.Contains(arg, "`") || strings.Contains(arg, "$(") {
			return fmt.Errorf("potentially dangerous argument detected: %s", arg)
		}
		
		// Prevent path traversal
		if strings.Contains(arg, "../") || strings.HasPrefix(arg, "/") {
			return fmt.Errorf("path traversal attempt detected: %s", arg)
		}
	}

	return nil
}

// isAllowedURL checks if a URL is safe to access
func (ve *ValidationEngine) isAllowedURL(url string) bool {
	allowedDomains := []string{
		"github.com",
		"gitlab.com",
		"bitbucket.org",
		"raw.githubusercontent.com",
		"api.github.com",
		"registry.npmjs.org",
		"pypi.org",
		"golang.org",
		"pkg.go.dev",
		"hub.docker.com",
		"registry.hub.docker.com",
	}

	for _, domain := range allowedDomains {
		if strings.Contains(url, domain) {
			return true
		}
	}
	return false
}

// Content validation methods

func (ve *ValidationEngine) validateRegexMatch(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	filePath := filepath.Join(workspacePath, rule.Condition.FilePath)
	
	content, err := os.ReadFile(filePath)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Cannot read file: %s", err.Error())
		return nil
	}

	contentStr := string(content)
	matched, err := regexp.MatchString(rule.Condition.Regex, contentStr)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	result.Details.FileInfo = &FileInfo{
		Exists:  true,
		Content: contentStr,
		Size:    int64(len(content)),
	}

	result.Passed = matched != rule.Condition.Negate
	result.Message = fmt.Sprintf("Regex pattern match: %t", matched)
	return nil
}

// Network validation methods

func (ve *ValidationEngine) validateHTTPResponse(ctx context.Context, rule *ValidationRule, result *ValidationResult) error {
	client := &http.Client{Timeout: 10 * time.Second}
	
	req, err := http.NewRequestWithContext(ctx, rule.Condition.Method, rule.Condition.URL, strings.NewReader(rule.Condition.RequestBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add headers
	for key, value := range rule.Condition.Headers {
		req.Header.Set(key, value)
	}

	startTime := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("HTTP request failed: %s", err.Error())
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	result.Details.HTTPResponse = &HTTPResponse{
		StatusCode:    resp.StatusCode,
		Headers:       make(map[string]string),
		Body:          bodyStr,
		ContentLength: resp.ContentLength,
		Duration:      duration.Milliseconds(),
	}

	// Copy headers
	for key, values := range resp.Header {
		if len(values) > 0 {
			result.Details.HTTPResponse.Headers[key] = values[0]
		}
	}

	// Check response body against expected pattern
	if rule.Condition.ResponseBody != "" {
		if rule.Condition.CaseSensitive {
			result.Passed = strings.Contains(bodyStr, rule.Condition.ResponseBody)
		} else {
			result.Passed = strings.Contains(strings.ToLower(bodyStr), strings.ToLower(rule.Condition.ResponseBody))
		}
		result.Message = fmt.Sprintf("HTTP response body contains expected content: %t", result.Passed)
	} else {
		result.Passed = resp.StatusCode >= 200 && resp.StatusCode < 300
		result.Message = fmt.Sprintf("HTTP request successful (status %d): %t", resp.StatusCode, result.Passed)
	}

	return nil
}

func (ve *ValidationEngine) validateHTTPStatusCode(ctx context.Context, rule *ValidationRule, result *ValidationResult) error {
	client := &http.Client{Timeout: 10 * time.Second}
	
	req, err := http.NewRequestWithContext(ctx, rule.Condition.Method, rule.Condition.URL, strings.NewReader(rule.Condition.RequestBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	startTime := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("HTTP request failed: %s", err.Error())
		return nil
	}
	defer resp.Body.Close()

	result.Details.HTTPResponse = &HTTPResponse{
		StatusCode: resp.StatusCode,
		Duration:   duration.Milliseconds(),
	}

	expectedStatusCode := rule.Condition.StatusCode
	result.Passed = resp.StatusCode == expectedStatusCode
	result.Message = fmt.Sprintf("HTTP status code %d matches expected %d: %t", resp.StatusCode, expectedStatusCode, result.Passed)
	return nil
}

// Script validation methods

func (ve *ValidationEngine) validateCustomScript(ctx context.Context, rule *ValidationRule, result *ValidationResult, workspacePath string) error {
	// Create temporary script file
	scriptFile := filepath.Join(workspacePath, fmt.Sprintf("validation_script_%s", uuid.New().String()[:8]))
	
	if err := os.WriteFile(scriptFile, []byte(rule.Condition.Script), 0755); err != nil {
		return fmt.Errorf("failed to write script file: %w", err)
	}
	defer os.Remove(scriptFile)

	// Execute script based on language
	var cmd *exec.Cmd
	switch rule.Condition.Language {
	case "bash", "sh":
		cmd = exec.CommandContext(ctx, "bash", scriptFile)
	case "python", "python3":
		cmd = exec.CommandContext(ctx, "python3", scriptFile)
	case "node", "javascript":
		cmd = exec.CommandContext(ctx, "node", scriptFile)
	default:
		cmd = exec.CommandContext(ctx, scriptFile)
	}

	cmd.Dir = workspacePath

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	outputStr := string(output)
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}

	result.Details.ScriptResult = &ScriptResult{
		Output:     outputStr,
		ReturnCode: exitCode,
		Duration:   duration.Milliseconds(),
	}

	if err != nil {
		result.Details.ScriptResult.Error = err.Error()
	}

	result.Passed = exitCode == 0
	result.Message = fmt.Sprintf("Custom script executed successfully: %t", result.Passed)
	return nil
}

// Helper methods

func (ve *ValidationEngine) getOrCreateTaskValidation(sessionID, userID, labID, taskID string) (*TaskValidation, error) {
	var taskValidation TaskValidation
	
	err := ve.db.Where("session_id = ? AND user_id = ? AND lab_id = ? AND task_id = ?", 
		sessionID, userID, labID, taskID).First(&taskValidation).Error
	
	if err == gorm.ErrRecordNotFound {
		taskValidation = TaskValidation{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			UserID:    userID,
			LabID:     labID,
			TaskID:    taskID,
			Status:    TaskValidationStatusPending,
			Attempts:  0,
		}
		if err := ve.db.Create(&taskValidation).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &taskValidation, nil
}

func (ve *ValidationEngine) createEmptyTaskValidation(sessionID, userID, labID, taskID string) *TaskValidation {
	return &TaskValidation{
		ID:         uuid.New().String(),
		SessionID:  sessionID,
		UserID:     userID,
		LabID:      labID,
		TaskID:     taskID,
		Status:     TaskValidationStatusCompleted,
		Percentage: 100.0,
		Feedback: TaskFeedback{
			Summary:       "No validation rules defined for this task",
			AutoGenerated: true,
		},
	}
}

func (ve *ValidationEngine) generateTaskFeedback(tv *TaskValidation, rules []ValidationRule) TaskFeedback {
	feedback := TaskFeedback{
		Summary:       "",
		Strengths:     []string{},
		Improvements:  []string{},
		NextSteps:     []string{},
		AutoGenerated: true,
		Grade:         tv.GetGrade(),
	}

	// Generate summary based on results
	if tv.IsSuccessful() {
		feedback.Summary = "Excellent work! You have successfully completed this task."
		feedback.Strengths = append(feedback.Strengths, "All required validations passed")
	} else if tv.Status == TaskValidationStatusPartial {
		feedback.Summary = "Good progress! Some validations passed, but there are areas for improvement."
		feedback.Improvements = append(feedback.Improvements, "Review the failed validations and make necessary corrections")
	} else {
		feedback.Summary = "This task needs more work. Please review the requirements and try again."
		feedback.NextSteps = append(feedback.NextSteps, "Carefully read the task requirements")
		feedback.NextSteps = append(feedback.NextSteps, "Check the validation errors for specific guidance")
	}

	// Add specific feedback based on failed rules
	if tv.FailedRules > 0 {
		feedback.Improvements = append(feedback.Improvements, "Focus on the areas that didn't pass validation")
	}

	return feedback
}

func (ve *ValidationEngine) publishValidationResult(ctx context.Context, result *ValidationResult) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "validation.result",
		Source:    "examiner",
		SessionID: result.SessionID,
		UserID:    result.UserID,
		Data: map[string]interface{}{
			"validation_result": result,
			"rule_id":          result.RuleID,
			"task_id":          result.TaskID,
			"passed":           result.Passed,
			"score":            result.Score,
		},
		Timestamp: time.Now(),
	}

	if err := ve.eventBus.Publish(ctx, event); err != nil {
		// Log error but don't fail validation
		fmt.Printf("Failed to publish validation result event: %v\n", err)
	}
}

func (ve *ValidationEngine) publishTaskValidationComplete(ctx context.Context, tv *TaskValidation) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "task.validation_complete",
		Source:    "examiner",
		SessionID: tv.SessionID,
		UserID:    tv.UserID,
		Data: map[string]interface{}{
			"task_validation": tv,
			"task_id":         tv.TaskID,
			"lab_id":          tv.LabID,
			"status":          tv.Status,
			"score":           tv.Score,
			"percentage":      tv.Percentage,
		},
		Timestamp: time.Now(),
	}

	if err := ve.eventBus.Publish(ctx, event); err != nil {
		fmt.Printf("Failed to publish task validation complete event: %v\n", err)
	}
}

func (ve *ValidationEngine) logValidationError(ctx context.Context, sessionID, userID, ruleID string, err error) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "validation.error",
		Source:    "examiner",
		SessionID: sessionID,
		UserID:    userID,
		Data: map[string]interface{}{
			"rule_id": ruleID,
			"error":   err.Error(),
		},
		Timestamp: time.Now(),
	}

	if publishErr := ve.eventBus.Publish(ctx, event); publishErr != nil {
		fmt.Printf("Failed to publish validation error event: %v\n", publishErr)
	}
}