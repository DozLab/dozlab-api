package examiner

import (
	"context"
	"fmt"
	"time"

	"dozlab-backend/internal/websocket"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TaskValidator handles task completion validation
type TaskValidator struct {
	db       *gorm.DB
	eventBus *websocket.RedisEventBus
	engine   *ValidationEngine
}

// NewTaskValidator creates a new task validator
func NewTaskValidator(db *gorm.DB, eventBus *websocket.RedisEventBus) *TaskValidator {
	return &TaskValidator{
		db:       db,
		eventBus: eventBus,
		engine:   NewValidationEngine(db, eventBus),
	}
}

// ValidateLabCompletion validates completion of an entire lab
func (tv *TaskValidator) ValidateLabCompletion(ctx context.Context, sessionID, userID, labID string, workspacePath string) (*LabValidation, error) {
	// Get all tasks for the lab
	var tasks []string
	if err := tv.db.Model(&ValidationRule{}).
		Where("lab_id = ?", labID).
		Distinct("task_id").
		Pluck("task_id", &tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to get lab tasks: %w", err)
	}

	if len(tasks) == 0 {
		return tv.createEmptyLabValidation(sessionID, userID, labID), nil
	}

	// Create or get existing lab validation
	labValidation, err := tv.getOrCreateLabValidation(sessionID, userID, labID)
	if err != nil {
		return nil, fmt.Errorf("failed to create lab validation: %w", err)
	}

	labValidation.TotalTasks = int32(len(tasks))
	labValidation.StartedAt = time.Now()

	// Validate each task
	var totalScore, maxScore float64
	var completedTasks, failedTasks, partialTasks int32

	taskValidations := make([]*TaskValidation, 0, len(tasks))

	for _, taskID := range tasks {
		taskValidation, err := tv.engine.ValidateTask(ctx, sessionID, userID, labID, taskID, workspacePath)
		if err != nil {
			tv.logTaskValidationError(ctx, sessionID, userID, labID, taskID, err)
			failedTasks++
			continue
		}

		taskValidations = append(taskValidations, taskValidation)
		
		totalScore += taskValidation.Score
		maxScore += taskValidation.MaxScore

		switch taskValidation.Status {
		case TaskValidationStatusCompleted:
			completedTasks++
		case TaskValidationStatusPartial:
			partialTasks++
		default:
			failedTasks++
		}
	}

	// Update lab validation with results
	labValidation.CompletedTasks = completedTasks
	labValidation.FailedTasks = failedTasks
	labValidation.PartialTasks = partialTasks
	labValidation.TotalScore = totalScore
	labValidation.MaxScore = maxScore

	if maxScore > 0 {
		labValidation.FinalPercentage = (totalScore / maxScore) * 100
	}

	// Determine overall status
	if failedTasks == 0 && partialTasks == 0 {
		labValidation.Status = LabValidationStatusCompleted
	} else if completedTasks > 0 || partialTasks > 0 {
		labValidation.Status = LabValidationStatusCompleted // Partial completion still counts as completed
	} else {
		labValidation.Status = LabValidationStatusFailed
	}

	// Set completion time and generate feedback
	now := time.Now()
	labValidation.CompletedAt = &now
	labValidation.Duration = int32(now.Sub(labValidation.StartedAt).Minutes())
	labValidation.Grade = labValidation.GetOverallGrade()
	labValidation.Feedback = tv.generateLabFeedback(labValidation, taskValidations)

	// Generate certificate if lab is successfully completed
	if labValidation.IsSuccessful() {
		labValidation.Certificate = tv.generateCertificate(labValidation)
	}

	if err := tv.db.Save(labValidation).Error; err != nil {
		return nil, fmt.Errorf("failed to save lab validation: %w", err)
	}

	// Publish lab completion event
	tv.publishLabValidationComplete(ctx, labValidation)

	return labValidation, nil
}

// ValidateTaskCompletion validates completion of a specific task
func (tv *TaskValidator) ValidateTaskCompletion(ctx context.Context, sessionID, userID, labID, taskID string, workspacePath string) (*TaskValidation, error) {
	return tv.engine.ValidateTask(ctx, sessionID, userID, labID, taskID, workspacePath)
}

// CheckTaskPrerequisites checks if task prerequisites are met
func (tv *TaskValidator) CheckTaskPrerequisites(ctx context.Context, sessionID, userID, labID, taskID string) (bool, []string, error) {
	// This would check if previous tasks are completed
	// For now, return true (no prerequisites)
	return true, []string{}, nil
}

// GetTaskValidationStatus gets current validation status for a task
func (tv *TaskValidator) GetTaskValidationStatus(ctx context.Context, sessionID, userID, labID, taskID string) (*TaskValidation, error) {
	var taskValidation TaskValidation
	
	err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ? AND task_id = ?", 
		sessionID, userID, labID, taskID).First(&taskValidation).Error
	
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("task validation not found")
	}
	
	return &taskValidation, err
}

// GetLabValidationStatus gets current validation status for a lab
func (tv *TaskValidator) GetLabValidationStatus(ctx context.Context, sessionID, userID, labID string) (*LabValidation, error) {
	var labValidation LabValidation
	
	err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ?", 
		sessionID, userID, labID).First(&labValidation).Error
	
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("lab validation not found")
	}
	
	return &labValidation, err
}

// GetValidationResults gets all validation results for a task
func (tv *TaskValidator) GetValidationResults(ctx context.Context, sessionID, userID, labID, taskID string) ([]ValidationResult, error) {
	var results []ValidationResult
	
	err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ? AND task_id = ?", 
		sessionID, userID, labID, taskID).
		Order("started_at DESC").
		Find(&results).Error
	
	return results, err
}

// RetryTaskValidation retries validation for a specific task
func (tv *TaskValidator) RetryTaskValidation(ctx context.Context, sessionID, userID, labID, taskID string, workspacePath string) (*TaskValidation, error) {
	// Clear previous validation results for this attempt
	if err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ? AND task_id = ?", 
		sessionID, userID, labID, taskID).Delete(&ValidationResult{}).Error; err != nil {
		return nil, fmt.Errorf("failed to clear previous results: %w", err)
	}

	return tv.ValidateTaskCompletion(ctx, sessionID, userID, labID, taskID, workspacePath)
}

// GetValidationProgress gets real-time validation progress
func (tv *TaskValidator) GetValidationProgress(ctx context.Context, sessionID, userID, labID string) (*ValidationProgress, error) {
	var progress ValidationProgress
	progress.SessionID = sessionID
	progress.UserID = userID
	progress.LabID = labID
	progress.UpdatedAt = time.Now()

	// Get task validations
	var taskValidations []TaskValidation
	if err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ?", 
		sessionID, userID, labID).Find(&taskValidations).Error; err != nil {
		return nil, fmt.Errorf("failed to get task validations: %w", err)
	}

	progress.Tasks = make([]TaskProgress, len(taskValidations))
	for i, tv := range taskValidations {
		progress.Tasks[i] = TaskProgress{
			TaskID:     tv.TaskID,
			Status:     string(tv.Status),
			Score:      tv.Score,
			MaxScore:   tv.MaxScore,
			Percentage: tv.Percentage,
			Passed:     tv.PassedRules,
			Failed:     tv.FailedRules,
			Total:      tv.TotalRules,
		}
	}

	// Calculate overall progress
	var totalScore, maxScore float64
	var completedTasks int
	for _, task := range progress.Tasks {
		totalScore += task.Score
		maxScore += task.MaxScore
		if task.Status == string(TaskValidationStatusCompleted) {
			completedTasks++
		}
	}

	if maxScore > 0 {
		progress.OverallPercentage = (totalScore / maxScore) * 100
	}
	progress.CompletedTasks = completedTasks
	progress.TotalTasks = len(progress.Tasks)

	return &progress, nil
}

// Helper methods

func (tv *TaskValidator) getOrCreateLabValidation(sessionID, userID, labID string) (*LabValidation, error) {
	var labValidation LabValidation
	
	err := tv.db.Where("session_id = ? AND user_id = ? AND lab_id = ?", 
		sessionID, userID, labID).First(&labValidation).Error
	
	if err == gorm.ErrRecordNotFound {
		labValidation = LabValidation{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			UserID:    userID,
			LabID:     labID,
			Status:    LabValidationStatusInProgress,
		}
		if err := tv.db.Create(&labValidation).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &labValidation, nil
}

func (tv *TaskValidator) createEmptyLabValidation(sessionID, userID, labID string) *LabValidation {
	now := time.Now()
	return &LabValidation{
		ID:              uuid.New().String(),
		SessionID:       sessionID,
		UserID:          userID,
		LabID:           labID,
		Status:          LabValidationStatusCompleted,
		FinalPercentage: 100.0,
		Grade:           "A",
		StartedAt:       now,
		CompletedAt:     &now,
		Duration:        1,
		Feedback: LabFeedback{
			OverallSummary: "No validation rules defined for this lab",
		},
	}
}

func (tv *TaskValidator) generateLabFeedback(lv *LabValidation, taskValidations []*TaskValidation) LabFeedback {
	feedback := LabFeedback{
		TaskSummaries:      make(map[string]string),
		SkillsAssessed:     []SkillAssessment{},
		LearningObjectives: make(map[string]ObjectiveStatus),
		Recommendations:    []string{},
		TimeAnalysis: TimeAnalysis{
			TotalTime:   lv.Duration,
			TimePerTask: make(map[string]int32),
		},
	}

	// Generate overall summary
	if lv.IsSuccessful() {
		feedback.OverallSummary = "Congratulations! You have successfully completed this lab with excellent results."
	} else if lv.FinalPercentage >= 60 {
		feedback.OverallSummary = "Good effort! You have completed most of the lab requirements."
	} else {
		feedback.OverallSummary = "This lab needs more work. Please review the requirements and retry the tasks."
	}

	// Generate task summaries
	for _, taskValidation := range taskValidations {
		if taskValidation.IsSuccessful() {
			feedback.TaskSummaries[taskValidation.TaskID] = "Task completed successfully"
		} else {
			feedback.TaskSummaries[taskValidation.TaskID] = "Task needs improvement"
		}
		feedback.TimeAnalysis.TimePerTask[taskValidation.TaskID] = taskValidation.CompletionTime
	}

	// Add skill assessments based on performance
	if lv.FinalPercentage >= 90 {
		feedback.SkillsAssessed = append(feedback.SkillsAssessed, SkillAssessment{
			Skill:       "Problem Solving",
			Level:       "Advanced",
			Proficiency: lv.FinalPercentage,
			Evidence:    "Excellent completion of all lab tasks",
		})
	} else if lv.FinalPercentage >= 70 {
		feedback.SkillsAssessed = append(feedback.SkillsAssessed, SkillAssessment{
			Skill:       "Technical Implementation",
			Level:       "Intermediate",
			Proficiency: lv.FinalPercentage,
			Evidence:    "Good completion of most lab tasks",
		})
	}

	// Calculate efficiency score
	if lv.Duration > 0 {
		// Simple efficiency calculation - could be more sophisticated
		expectedTime := float64(lv.TotalTasks * 15) // 15 minutes per task
		actualTime := float64(lv.Duration)
		efficiency := (expectedTime / actualTime) * 100
		if efficiency > 100 {
			efficiency = 100
		}
		feedback.TimeAnalysis.EfficiencyScore = efficiency
	}

	// Add recommendations based on performance
	if lv.FinalPercentage < 70 {
		feedback.Recommendations = append(feedback.Recommendations, "Review the lab materials and practice the concepts")
		feedback.Recommendations = append(feedback.Recommendations, "Consider retrying the failed tasks")
	} else if lv.FinalPercentage < 90 {
		feedback.Recommendations = append(feedback.Recommendations, "Great work! Consider exploring advanced topics in this area")
	} else {
		feedback.Recommendations = append(feedback.Recommendations, "Excellent performance! You're ready for more challenging labs")
	}

	return feedback
}

func (tv *TaskValidator) generateCertificate(lv *LabValidation) *Certificate {
	return &Certificate{
		ID:         uuid.New().String(),
		Title:      fmt.Sprintf("Lab Completion Certificate - %s", lv.LabID),
		Recipient:  lv.UserID,
		Issuer:     "DozLab Platform",
		IssuedDate: time.Now(),
		URL:        fmt.Sprintf("https://dozlab.com/certificates/%s", lv.ID),
		Hash:       generateCertificateHash(lv),
		Blockchain: false,
	}
}

func generateCertificateHash(lv *LabValidation) string {
	// Simple hash generation - should be more sophisticated in production
	return fmt.Sprintf("cert_%s_%s_%d", lv.UserID, lv.LabID, lv.CompletedAt.Unix())
}

func (tv *TaskValidator) publishLabValidationComplete(ctx context.Context, lv *LabValidation) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "lab.validation_complete",
		Source:    "examiner",
		SessionID: lv.SessionID,
		UserID:    lv.UserID,
		Data: map[string]interface{}{
			"lab_validation":    lv,
			"lab_id":           lv.LabID,
			"status":           lv.Status,
			"final_percentage": lv.FinalPercentage,
			"grade":            lv.Grade,
			"certificate":      lv.Certificate,
		},
		Timestamp: time.Now(),
	}

	if err := tv.eventBus.Publish(ctx, event); err != nil {
		fmt.Printf("Failed to publish lab validation complete event: %v\n", err)
	}
}

func (tv *TaskValidator) logTaskValidationError(ctx context.Context, sessionID, userID, labID, taskID string, err error) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "task.validation_error",
		Source:    "examiner",
		SessionID: sessionID,
		UserID:    userID,
		Data: map[string]interface{}{
			"lab_id":  labID,
			"task_id": taskID,
			"error":   err.Error(),
		},
		Timestamp: time.Now(),
	}

	if err := tv.eventBus.Publish(ctx, event); err != nil {
		fmt.Printf("Failed to publish task validation error event: %v\n", err)
	}
}

// ValidationProgress represents real-time validation progress
type ValidationProgress struct {
	SessionID         string         `json:"session_id"`
	UserID            string         `json:"user_id"`
	LabID             string         `json:"lab_id"`
	Tasks             []TaskProgress `json:"tasks"`
	TotalTasks        int            `json:"total_tasks"`
	CompletedTasks    int            `json:"completed_tasks"`
	OverallPercentage float64        `json:"overall_percentage"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// TaskProgress represents progress for a single task
type TaskProgress struct {
	TaskID     string  `json:"task_id"`
	Status     string  `json:"status"`
	Score      float64 `json:"score"`
	MaxScore   float64 `json:"max_score"`
	Percentage float64 `json:"percentage"`
	Passed     int32   `json:"passed"`
	Failed     int32   `json:"failed"`
	Total      int32   `json:"total"`
}