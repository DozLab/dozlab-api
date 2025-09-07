package handlers

import (
	"net/http"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/websocket"
	"dozlab-backend/internal/workflow"

	"github.com/gin-gonic/gin"
)

// WorkflowHandler handles workflow-related API endpoints
type WorkflowHandler struct {
	db             *database.Database
	cfg            *config.Config
	workflowService *workflow.WorkflowService
}

// NewWorkflowHandler creates a new workflow handler
func NewWorkflowHandler(db *database.Database, cfg *config.Config, wsService *websocket.SessionService) *WorkflowHandler {
	return &WorkflowHandler{
		db:              db,
		cfg:             cfg,
		workflowService: workflow.NewWorkflowService(db, wsService),
	}
}

// CreateWorkflow creates a new workflow
// @Summary Create workflow
// @Description Create a new DAG workflow for a lab
// @Tags workflow
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Workflow creation request"
// @Success 201 {object} map[string]interface{} "Workflow created successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 404 {object} map[string]interface{} "Lab not found"
// @Failure 500 {object} map[string]interface{} "Creation failed"
// @Router /api/v1/workflows [post]
func (h *WorkflowHandler) CreateWorkflow(c *gin.Context) {
	var req workflow.CreateWorkflowRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	createdWorkflow, err := h.workflowService.CreateWorkflow(c.Request.Context(), &req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "lab not found" {
			statusCode = http.StatusNotFound
		} else if err.Error() == "invalid workflow DAG" {
			statusCode = http.StatusBadRequest
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to create workflow: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Workflow created successfully",
		"data":    createdWorkflow,
	})
}

// ExecuteWorkflow starts workflow execution
// @Summary Execute workflow
// @Description Start execution of a DAG workflow
// @Tags workflow
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Workflow execution request"
// @Success 200 {object} map[string]interface{} "Workflow execution started"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 404 {object} map[string]interface{} "Workflow not found"
// @Failure 500 {object} map[string]interface{} "Execution failed"
// @Router /api/v1/workflows/execute [post]
func (h *WorkflowHandler) ExecuteWorkflow(c *gin.Context) {
	var req workflow.ExecuteWorkflowRequest

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
	req.UserID = userID.(string)

	execution, err := h.workflowService.ExecuteWorkflow(c.Request.Context(), &req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "workflow not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to execute workflow: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workflow execution started",
		"data":    execution,
	})
}

// GetWorkflowExecution gets workflow execution status
// @Summary Get workflow execution
// @Description Get the status and progress of a workflow execution
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param execution_id path string true "Workflow Execution ID"
// @Success 200 {object} map[string]interface{} "Workflow execution details"
// @Failure 404 {object} map[string]interface{} "Execution not found"
// @Failure 500 {object} map[string]interface{} "Failed to get execution"
// @Router /api/v1/workflows/executions/{execution_id} [get]
func (h *WorkflowHandler) GetWorkflowExecution(c *gin.Context) {
	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Execution ID is required",
		})
		return
	}

	execution, err := h.workflowService.GetWorkflowExecution(c.Request.Context(), executionID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "workflow execution not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to get workflow execution: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    execution,
	})
}

// GetWorkflowProgress gets detailed workflow progress
// @Summary Get workflow progress
// @Description Get detailed progress information for a workflow execution
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param execution_id path string true "Workflow Execution ID"
// @Success 200 {object} map[string]interface{} "Detailed progress information"
// @Failure 404 {object} map[string]interface{} "Execution not found"
// @Failure 500 {object} map[string]interface{} "Failed to get progress"
// @Router /api/v1/workflows/executions/{execution_id}/progress [get]
func (h *WorkflowHandler) GetWorkflowProgress(c *gin.Context) {
	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Execution ID is required",
		})
		return
	}

	progress, err := h.workflowService.GetWorkflowProgress(c.Request.Context(), executionID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "workflow execution not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to get workflow progress: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    progress,
	})
}

// StopWorkflowExecution stops a running workflow execution
// @Summary Stop workflow execution
// @Description Stop a running workflow execution
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param execution_id path string true "Workflow Execution ID"
// @Success 200 {object} map[string]interface{} "Workflow execution stopped"
// @Failure 404 {object} map[string]interface{} "Execution not found"
// @Failure 500 {object} map[string]interface{} "Failed to stop execution"
// @Router /api/v1/workflows/executions/{execution_id}/stop [post]
func (h *WorkflowHandler) StopWorkflowExecution(c *gin.Context) {
	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Execution ID is required",
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

	err := h.workflowService.StopWorkflowExecution(c.Request.Context(), executionID, userID.(string))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "workflow execution not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"success": false,
			"error":   "Failed to stop workflow execution: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workflow execution stopped successfully",
	})
}

// ListWorkflowExecutions lists workflow executions for the authenticated user
// @Summary List workflow executions
// @Description Get all workflow executions for the authenticated user
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param session_id query string false "Session ID to filter executions"
// @Success 200 {array} map[string]interface{} "List of workflow executions"
// @Failure 500 {object} map[string]interface{} "Failed to list executions"
// @Router /api/v1/workflows/executions [get]
func (h *WorkflowHandler) ListWorkflowExecutions(c *gin.Context) {
	// Get user ID from JWT middleware
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	sessionID := c.Query("session_id")

	executions, err := h.workflowService.ListWorkflowExecutions(c.Request.Context(), userID.(string), sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to list workflow executions: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    executions,
		"count":   len(executions),
	})
}

// ValidateWorkflow validates a workflow definition
// @Summary Validate workflow
// @Description Validate a workflow definition and return DAG analysis
// @Tags workflow
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body map[string]interface{} true "Workflow to validate"
// @Success 200 {object} map[string]interface{} "Workflow validation results"
// @Failure 400 {object} map[string]interface{} "Validation failed"
// @Router /api/v1/workflows/validate [post]
func (h *WorkflowHandler) ValidateWorkflow(c *gin.Context) {
	var workflowToValidate workflow.Workflow

	if err := c.ShouldBindJSON(&workflowToValidate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid workflow definition: " + err.Error(),
		})
		return
	}

	analysis, err := h.workflowService.ValidateWorkflow(c.Request.Context(), &workflowToValidate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Workflow validation failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workflow validation completed",
		"data":    analysis,
	})
}

// GetWorkflowTemplate gets workflow templates for common lab patterns
// @Summary Get workflow template
// @Description Get a workflow template for common lab patterns
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param template_type path string true "Template type (coding-exercise, deployment-lab, security-assessment)"
// @Success 200 {object} map[string]interface{} "Workflow template"
// @Failure 400 {object} map[string]interface{} "Invalid template type"
// @Router /api/v1/workflows/templates/{template_type} [get]
func (h *WorkflowHandler) GetWorkflowTemplate(c *gin.Context) {
	templateType := c.Param("template_type")
	if templateType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Template type is required",
		})
		return
	}

	template, err := h.workflowService.GetWorkflowTemplate(templateType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Failed to get workflow template: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    template,
	})
}

// GetWorkflowDAGVisualization gets DAG visualization data
// @Summary Get workflow DAG visualization
// @Description Get DAG visualization data for a workflow
// @Tags workflow
// @Produce json
// @Security Bearer
// @Param workflow_id path string true "Workflow ID"
// @Success 200 {object} map[string]interface{} "DAG visualization data"
// @Failure 404 {object} map[string]interface{} "Workflow not found"
// @Router /api/v1/workflows/{workflow_id}/dag [get]
func (h *WorkflowHandler) GetWorkflowDAGVisualization(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Workflow ID is required",
		})
		return
	}

	// For this example, create sample DAG visualization data
	// In production, this would load the workflow and generate visualization
	
	dagVisualization := map[string]interface{}{
		"workflow_id": workflowID,
		"nodes": []map[string]interface{}{
			{
				"id":           "task-1",
				"name":         "Setup Environment",
				"type":         "command",
				"level":        0,
				"critical_path": true,
				"status":       "completed",
			},
			{
				"id":           "task-2",
				"name":         "Install Dependencies", 
				"type":         "command",
				"level":        1,
				"critical_path": true,
				"status":       "running",
			},
			{
				"id":           "task-3",
				"name":         "Run Tests",
				"type":         "command",
				"level":        2,
				"critical_path": true,
				"status":       "pending",
			},
			{
				"id":           "task-4",
				"name":         "Validate Results",
				"type":         "validation",
				"level":        3,
				"critical_path": true,
				"status":       "pending",
			},
		},
		"edges": []map[string]interface{}{
			{"from": "task-1", "to": "task-2"},
			{"from": "task-2", "to": "task-3"},
			{"from": "task-3", "to": "task-4"},
		},
		"critical_path": []string{"task-1", "task-2", "task-3", "task-4"},
		"parallel_levels": [][]string{
			{"task-1"},
			{"task-2"},
			{"task-3"},
			{"task-4"},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dagVisualization,
	})
}