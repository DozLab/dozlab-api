package handlers

import (
	"net/http"
	"strconv"

	"dozlab-backend/internal/config"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/examiner"
	"dozlab-backend/internal/websocket"

	"github.com/gin-gonic/gin"
)

// ExaminerHandler handles examiner and validation API endpoints
type ExaminerHandler struct {
	db               *database.Database
	cfg              *config.Config
	validator        *examiner.TaskValidator
	feedbackService  *examiner.FeedbackService
	validationEngine *examiner.ValidationEngine
}

// NewExaminerHandler creates a new examiner handler
func NewExaminerHandler(db *database.Database, cfg *config.Config, eventBus websocket.EventPublisher) *ExaminerHandler {
	return &ExaminerHandler{
		db:               db,
		cfg:              cfg,
		validator:        examiner.NewTaskValidator(db.DB, eventBus),
		feedbackService:  examiner.NewFeedbackService(db.DB, eventBus),
		validationEngine: examiner.NewValidationEngine(db.DB, eventBus),
	}
}

// ValidateTask validates a specific task for completion
// @Summary Validate task
// @Description Validate task completion against defined validation rules
// @Tags examiner
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Task validation request"
// @Success 200 {object} map[string]interface{} "Task validation results"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Validation failed"
// @Router /api/v1/examiner/validate/task [post]
func (h *ExaminerHandler) ValidateTask(c *gin.Context) {
	var req struct {
		SessionID     string `json:"session_id" binding:"required"`
		LabID         string `json:"lab_id" binding:"required"`
		TaskID        string `json:"task_id" binding:"required"`
		WorkspacePath string `json:"workspace_path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Validate task
	taskValidation, err := h.validator.ValidateTaskCompletion(
		c.Request.Context(), 
		req.SessionID, 
		userID.(string), 
		req.LabID, 
		req.TaskID, 
		req.WorkspacePath,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to validate task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Task validation completed",
		"data":    taskValidation,
	})
}

// ValidateLab validates an entire lab for completion
// @Summary Validate lab
// @Description Validate complete lab against all task validation rules
// @Tags examiner
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Lab validation request"
// @Success 200 {object} map[string]interface{} "Lab validation results"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Validation failed"
// @Router /api/v1/examiner/validate/lab [post]
func (h *ExaminerHandler) ValidateLab(c *gin.Context) {
	var req struct {
		SessionID     string `json:"session_id" binding:"required"`
		LabID         string `json:"lab_id" binding:"required"`
		WorkspacePath string `json:"workspace_path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Validate lab
	labValidation, err := h.validator.ValidateLabCompletion(
		c.Request.Context(),
		req.SessionID,
		userID.(string),
		req.LabID,
		req.WorkspacePath,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to validate lab: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Lab validation completed",
		"data":    labValidation,
	})
}

// GetTaskValidationStatus gets current validation status for a task
// @Summary Get task validation status
// @Description Get current validation status and results for a specific task
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]interface{} "Task validation status"
// @Failure 404 {object} map[string]interface{} "Validation not found"
// @Failure 500 {object} map[string]interface{} "Failed to get status"
// @Router /api/v1/examiner/status/task/{session_id}/{lab_id}/{task_id} [get]
func (h *ExaminerHandler) GetTaskValidationStatus(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")
	taskID := c.Param("task_id")

	if sessionID == "" || labID == "" || taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID, Lab ID, and Task ID are required",
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	taskValidation, err := h.validator.GetTaskValidationStatus(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
		taskID,
	)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "task validation not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to get task validation status: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    taskValidation,
	})
}

// GetLabValidationStatus gets current validation status for a lab
// @Summary Get lab validation status
// @Description Get current validation status and results for a complete lab
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Success 200 {object} map[string]interface{} "Lab validation status"
// @Failure 404 {object} map[string]interface{} "Validation not found"
// @Failure 500 {object} map[string]interface{} "Failed to get status"
// @Router /api/v1/examiner/status/lab/{session_id}/{lab_id} [get]
func (h *ExaminerHandler) GetLabValidationStatus(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")

	if sessionID == "" || labID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID and Lab ID are required",
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	labValidation, err := h.validator.GetLabValidationStatus(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
	)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "lab validation not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to get lab validation status: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    labValidation,
	})
}

// GetValidationProgress gets real-time validation progress
// @Summary Get validation progress
// @Description Get real-time progress of validation for a lab session
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Success 200 {object} map[string]interface{} "Validation progress"
// @Failure 500 {object} map[string]interface{} "Failed to get progress"
// @Router /api/v1/examiner/progress/{session_id}/{lab_id} [get]
func (h *ExaminerHandler) GetValidationProgress(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")

	if sessionID == "" || labID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Session ID and Lab ID are required",
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	progress, err := h.validator.GetValidationProgress(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get validation progress: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    progress,
	})
}

// GetTaskSummaryFeedback gets comprehensive feedback for task completion
// @Summary Get task summary feedback
// @Description Get detailed feedback and analysis for completed task
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]interface{} "Task summary feedback"
// @Failure 404 {object} map[string]interface{} "Task validation not found"
// @Failure 500 {object} map[string]interface{} "Failed to generate feedback"
// @Router /api/v1/examiner/feedback/task/{session_id}/{lab_id}/{task_id} [get]
func (h *ExaminerHandler) GetTaskSummaryFeedback(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")
	taskID := c.Param("task_id")

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Get task validation first
	taskValidation, err := h.validator.GetTaskValidationStatus(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
		taskID,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task validation not found: " + err.Error(),
		})
		return
	}

	// Generate summary feedback
	feedback, err := h.feedbackService.GenerateTaskSummaryFeedback(
		c.Request.Context(),
		taskValidation,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to generate task summary feedback: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    feedback,
	})
}

// GetPersonalizedRecommendations gets AI-powered learning recommendations
// @Summary Get personalized recommendations
// @Description Get personalized learning recommendations based on user performance
// @Tags examiner
// @Produce json
// @Security Bearer
// @Success 200 {object} map[string]interface{} "Personalized recommendations"
// @Failure 500 {object} map[string]interface{} "Failed to generate recommendations"
// @Router /api/v1/examiner/recommendations [get]
func (h *ExaminerHandler) GetPersonalizedRecommendations(c *gin.Context) {
	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	recommendations, err := h.feedbackService.GeneratePersonalizedRecommendations(
		c.Request.Context(),
		userID.(string),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to generate personalized recommendations: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    recommendations,
	})
}

// GetProgressiveScore gets advanced scoring with bonuses
// @Summary Get progressive score
// @Description Get progressive scoring with difficulty, time, and consistency bonuses
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Success 200 {object} map[string]interface{} "Progressive score details"
// @Failure 500 {object} map[string]interface{} "Failed to calculate score"
// @Router /api/v1/examiner/score/{session_id}/{lab_id} [get]
func (h *ExaminerHandler) GetProgressiveScore(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	score, err := h.feedbackService.CalculateProgressiveScore(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to calculate progressive score: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    score,
	})
}

// GetLeaderboardFeedback gets competitive feedback and ranking
// @Summary Get leaderboard feedback
// @Description Get competitive feedback including ranking and achievements
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param lab_id path string true "Lab ID"
// @Success 200 {object} map[string]interface{} "Leaderboard feedback"
// @Failure 500 {object} map[string]interface{} "Failed to get leaderboard feedback"
// @Router /api/v1/examiner/leaderboard/{lab_id} [get]
func (h *ExaminerHandler) GetLeaderboardFeedback(c *gin.Context) {
	labID := c.Param("lab_id")

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	feedback, err := h.feedbackService.GetLeaderboardFeedback(
		c.Request.Context(),
		userID.(string),
		labID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get leaderboard feedback: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    feedback,
	})
}

// RetryTaskValidation retries validation for a specific task
// @Summary Retry task validation
// @Description Retry validation for a task, clearing previous results
// @Tags examiner
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Retry validation request"
// @Success 200 {object} map[string]interface{} "Task validation results"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Validation failed"
// @Router /api/v1/examiner/retry/task [post]
func (h *ExaminerHandler) RetryTaskValidation(c *gin.Context) {
	var req struct {
		SessionID     string `json:"session_id" binding:"required"`
		LabID         string `json:"lab_id" binding:"required"`
		TaskID        string `json:"task_id" binding:"required"`
		WorkspacePath string `json:"workspace_path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// Retry task validation
	taskValidation, err := h.validator.RetryTaskValidation(
		c.Request.Context(),
		req.SessionID,
		userID.(string),
		req.LabID,
		req.TaskID,
		req.WorkspacePath,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to retry task validation: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Task validation retried successfully",
		"data":    taskValidation,
	})
}

// GetValidationResults gets detailed validation results for a task
// @Summary Get validation results
// @Description Get detailed validation results for all rules in a task
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param session_id path string true "Session ID"
// @Param lab_id path string true "Lab ID"
// @Param task_id path string true "Task ID"
// @Success 200 {array} map[string]interface{} "Validation results"
// @Failure 500 {object} map[string]interface{} "Failed to get results"
// @Router /api/v1/examiner/results/{session_id}/{lab_id}/{task_id} [get]
func (h *ExaminerHandler) GetValidationResults(c *gin.Context) {
	sessionID := c.Param("session_id")
	labID := c.Param("lab_id")
	taskID := c.Param("task_id")

	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	results, err := h.validator.GetValidationResults(
		c.Request.Context(),
		sessionID,
		userID.(string),
		labID,
		taskID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get validation results: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    results,
		"count":   len(results),
	})
}

// CreateValidationRule creates a new validation rule
// @Summary Create validation rule
// @Description Create a new validation rule for a lab task
// @Tags examiner
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body examiner.ValidationRule true "Validation rule"
// @Success 201 {object} map[string]interface{} "Rule created successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "Failed to create rule"
// @Router /api/v1/examiner/rules [post]
func (h *ExaminerHandler) CreateValidationRule(c *gin.Context) {
	var rule examiner.ValidationRule

	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Set default values
	if rule.Weight == 0 {
		rule.Weight = 1.0
	}
	if rule.Timeout == 0 {
		rule.Timeout = 30
	}

	// Save rule
	if err := h.db.DB.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to create validation rule: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Validation rule created successfully",
		"data":    rule,
	})
}

// ListValidationRules lists validation rules for a lab/task
// @Summary List validation rules
// @Description List all validation rules for a specific lab or task
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param lab_id query string true "Lab ID"
// @Param task_id query string false "Task ID (optional)"
// @Param enabled query bool false "Filter by enabled status"
// @Success 200 {array} examiner.ValidationRule "List of validation rules"
// @Failure 500 {object} map[string]interface{} "Failed to list rules"
// @Router /api/v1/examiner/rules [get]
func (h *ExaminerHandler) ListValidationRules(c *gin.Context) {
	labID := c.Query("lab_id")
	if labID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Lab ID is required",
		})
		return
	}

	taskID := c.Query("task_id")
	enabledParam := c.Query("enabled")

	query := h.db.DB.Where("lab_id = ?", labID)
	
	if taskID != "" {
		query = query.Where("task_id = ?", taskID)
	}

	if enabledParam != "" {
		if enabled, err := strconv.ParseBool(enabledParam); err == nil {
			query = query.Where("enabled = ?", enabled)
		}
	}

	var rules []examiner.ValidationRule
	if err := query.Order("task_id ASC, \"order\" ASC").Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to list validation rules: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    rules,
		"count":   len(rules),
	})
}

// UpdateValidationRule updates an existing validation rule
// @Summary Update validation rule
// @Description Update an existing validation rule
// @Tags examiner
// @Accept json
// @Produce json
// @Security Bearer
// @Param rule_id path string true "Rule ID"
// @Param request body examiner.ValidationRule true "Updated validation rule"
// @Success 200 {object} map[string]interface{} "Rule updated successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 404 {object} map[string]interface{} "Rule not found"
// @Failure 500 {object} map[string]interface{} "Failed to update rule"
// @Router /api/v1/examiner/rules/{rule_id} [put]
func (h *ExaminerHandler) UpdateValidationRule(c *gin.Context) {
	ruleID := c.Param("rule_id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Rule ID is required",
		})
		return
	}

	var updateData examiner.ValidationRule
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	// Update rule
	result := h.db.DB.Model(&examiner.ValidationRule{}).Where("id = ?", ruleID).Updates(&updateData)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to update validation rule: " + result.Error.Error(),
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Validation rule not found",
		})
		return
	}

	// Get updated rule
	var updatedRule examiner.ValidationRule
	if err := h.db.DB.First(&updatedRule, "id = ?", ruleID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get updated rule: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Validation rule updated successfully",
		"data":    updatedRule,
	})
}

// DeleteValidationRule deletes a validation rule
// @Summary Delete validation rule
// @Description Delete an existing validation rule
// @Tags examiner
// @Produce json
// @Security Bearer
// @Param rule_id path string true "Rule ID"
// @Success 200 {object} map[string]interface{} "Rule deleted successfully"
// @Failure 404 {object} map[string]interface{} "Rule not found"
// @Failure 500 {object} map[string]interface{} "Failed to delete rule"
// @Router /api/v1/examiner/rules/{rule_id} [delete]
func (h *ExaminerHandler) DeleteValidationRule(c *gin.Context) {
	ruleID := c.Param("rule_id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Rule ID is required",
		})
		return
	}

	result := h.db.DB.Delete(&examiner.ValidationRule{}, "id = ?", ruleID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to delete validation rule: " + result.Error.Error(),
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Validation rule not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Validation rule deleted successfully",
	})
}