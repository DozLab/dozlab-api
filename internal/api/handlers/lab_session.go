package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type LabSessionHandler struct {
	db        *database.Database
	k8sClient dynamic.Interface
	labSessionGVR schema.GroupVersionResource
}

func NewLabSessionHandler(db *database.Database, k8sClient dynamic.Interface) *LabSessionHandler {
	return &LabSessionHandler{
		db:        db,
		k8sClient: k8sClient,
		labSessionGVR: schema.GroupVersionResource{
			Group:    "dozlab.io",
			Version:  "v1",
			Resource: "labsessions",
		},
	}
}

// CreateLabSession creates a new lab session using CRDs
// @Summary Create a new lab session
// @Description Creates a new lab session environment using Kubernetes CRDs
// @Tags lab-sessions
// @Accept json
// @Produce json
// @Param request body object true "Lab session creation request"
// @Success 202 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/lab-sessions [post]
func (h *LabSessionHandler) CreateLabSession(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	type ResourceConfig struct {
		Memory  string `json:"memory,omitempty"`
		CPU     string `json:"cpu,omitempty"`
		Storage string `json:"storage,omitempty"`
	}

	type SessionConfig struct {
		VSCodePassword string `json:"vscode_password,omitempty"`
		EnableTerminal bool   `json:"enable_terminal,omitempty"`
		EnableVSCode   bool   `json:"enable_vscode,omitempty"`
		EnableSSH      bool   `json:"enable_ssh,omitempty"`
	}

	type CreateLabSessionRequest struct {
		LabID          uuid.UUID `json:"lab_id" binding:"required"`
		Resources      ResourceConfig `json:"resources,omitempty"`
		Config         SessionConfig `json:"config,omitempty"`
		Timeout        string `json:"timeout,omitempty"`
	}

	var req CreateLabSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Verify lab exists and user has access
	var lab models.Lab
	if err := h.db.DB.First(&lab, "id = ?", req.LabID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Lab not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database error",
		})
		return
	}

	// Check if lab is published for non-admin users
	userRole, _ := c.Get("role")
	if userRole != "admin" && !lab.IsPublished {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Lab is not published",
		})
		return
	}

	// Check for existing active session
	var existingSession models.Session
	if err := h.db.DB.Where("user_id = ? AND lab_id = ? AND status IN (?)", 
		userID, req.LabID, []string{SessionStatusPending, SessionStatusRunning}).First(&existingSession).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"error": "You already have an active session for this lab",
			"session_id": existingSession.ID,
		})
		return
	}

	// Create session record
	sessionID := uuid.New()
	session := models.Session{
		ID:       sessionID,
		UserID:   userID.(uuid.UUID),
		LabID:    req.LabID,
		Status:   SessionStatusPending,
	}

	if err := h.db.DB.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create session record",
		})
		return
	}

	// Set default values
	if req.Resources.Memory == "" {
		req.Resources.Memory = "4Gi"
	}
	if req.Resources.CPU == "" {
		req.Resources.CPU = "2"
	}
	if req.Resources.Storage == "" {
		req.Resources.Storage = "10Gi"
	}
	if req.Config.VSCodePassword == "" {
		req.Config.VSCodePassword = generatePassword()
	}
	if req.Timeout == "" {
		req.Timeout = "2h"
	}

	// Create LabSession CRD
	labSession := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "dozlab.io/v1",
			"kind":       "LabSession",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("session-%s", sessionID.String()),
				"namespace": "default",
				"labels": map[string]interface{}{
					"user-id":    userID.(uuid.UUID).String(),
					"session-id": sessionID.String(),
				},
			},
			"spec": map[string]interface{}{
				"userId":    userID.(uuid.UUID).String(),
				"sessionId": sessionID.String(),
				"resources": map[string]interface{}{
					"memory":  req.Resources.Memory,
					"cpu":     req.Resources.CPU,
					"storage": req.Resources.Storage,
				},
				"config": map[string]interface{}{
					"vsCodePassword": req.Config.VSCodePassword,
					"enableTerminal": true,
					"enableVSCode":   true,
					"enableSSH":      true,
				},
				"timeout": req.Timeout,
			},
		},
	}

	// Create lab session in Kubernetes
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := h.k8sClient.Resource(h.labSessionGVR).Namespace("default").Create(ctx, labSession, metav1.CreateOptions{})
	if err != nil {
		// Update session status to failed
		h.db.DB.Model(&session).Update("status", SessionStatusFailed)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create lab session",
			"details": err.Error(),
		})
		return
	}

	// The session stays pending until the controller reports Running

	// Load the session with relationships
	h.db.DB.Preload("Lab").Preload("User").First(&session, session.ID)

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Lab session creation started",
		"session": session,
		"credentials": gin.H{
			"vscode_password": req.Config.VSCodePassword,
		},
	})
}

// GetLabSession retrieves a lab session status
// @Summary Get lab session details
// @Description Retrieves details and status of a lab session
// @Tags lab-sessions
// @Accept json
// @Produce json
// @Param id path string true "Session ID"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/lab-sessions/{id} [get]
func (h *LabSessionHandler) GetLabSession(c *gin.Context) {
	sessionIDStr := c.Param("id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid session ID",
		})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	// Get session from database
	var session models.Session
	query := h.db.DB.Preload("Lab").Preload("User")

	// Non-admin users can only access their own sessions
	userRole, _ := c.Get("role")
	if userRole != "admin" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Where("id = ?", sessionID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Session not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database error",
		})
		return
	}

	// Get Kubernetes status if session is active
	var k8sStatus *K8sSessionStatus
	if isActiveSessionStatus(session.Status) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if k8sSession, err := h.k8sClient.Resource(h.labSessionGVR).Namespace("default").Get(ctx, fmt.Sprintf("session-%s", sessionID.String()), metav1.GetOptions{}); err == nil {
			k8sStatus = convertK8sStatus(k8sSession)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"session":    session,
		"k8s_status": k8sStatus,
	})
}

// ListLabSessions lists lab sessions for the current user
// @Summary List lab sessions
// @Description Lists all lab sessions for the authenticated user
// @Tags lab-sessions
// @Accept json
// @Produce json
// @Param status query string false "Filter by status"
// @Param lab_id query string false "Filter by lab ID"
// @Success 200 {object} object
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/lab-sessions [get]
func (h *LabSessionHandler) ListLabSessions(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	var sessions []models.Session
	query := h.db.DB.Preload("Lab").Preload("User")

	// Non-admin users can only see their own sessions
	userRole, _ := c.Get("role")
	if userRole != "admin" {
		query = query.Where("user_id = ?", userID)
	}

	// Apply filters
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if labIDStr := c.Query("lab_id"); labIDStr != "" {
		if labID, err := uuid.Parse(labIDStr); err == nil {
			query = query.Where("lab_id = ?", labID)
		}
	}

	if err := query.Order("created_at DESC").Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch sessions",
		})
		return
	}

	// Sync status with Kubernetes for active sessions
	h.syncSessionStatuses(sessions)

	c.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
	})
}

// DeleteLabSession terminates a lab session
// @Summary Delete lab session
// @Description Terminates and deletes a lab session
// @Tags lab-sessions
// @Accept json
// @Produce json
// @Param id path string true "Session ID"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/lab-sessions/{id} [delete]
func (h *LabSessionHandler) DeleteLabSession(c *gin.Context) {
	sessionIDStr := c.Param("id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid session ID",
		})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	// Get session
	var session models.Session
	query := h.db.DB

	// Non-admin users can only delete their own sessions
	userRole, _ := c.Get("role")
	if userRole != "admin" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Where("id = ?", sessionID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Session not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database error",
		})
		return
	}

	// Delete from Kubernetes unless an earlier delete already did
	if needsK8sDelete(session.Status) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := h.k8sClient.Resource(h.labSessionGVR).Namespace("default").Delete(ctx, fmt.Sprintf("session-%s", sessionID.String()), metav1.DeleteOptions{}); err != nil {
			// Log error but continue with database cleanup
			if !k8serrors.IsNotFound(err) {
				fmt.Printf("Failed to delete K8s resources: %v\n", err)
			}
		}
	}

	// Update session status
	if err := h.db.DB.Model(&session).Update("status", SessionStatusCompleted).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update session status",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Lab session terminated successfully",
	})
}

// Helper types and functions

type K8sSessionStatus struct {
	Phase     string                 `json:"phase"`
	Message   string                 `json:"message"`
	PodName   string                 `json:"pod_name,omitempty"`
	PodIP     string                 `json:"pod_ip,omitempty"`
	VMIP      string                 `json:"vm_ip,omitempty"`
	Endpoints map[string]string      `json:"endpoints,omitempty"`
	Conditions []K8sCondition        `json:"conditions,omitempty"`
}

type K8sCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func convertK8sStatus(k8sSession *unstructured.Unstructured) *K8sSessionStatus {
	status := &K8sSessionStatus{}
	
	// Extract phase
	if phase, found, _ := unstructured.NestedString(k8sSession.Object, "status", "phase"); found {
		status.Phase = phase
	}
	
	// Extract message
	if message, found, _ := unstructured.NestedString(k8sSession.Object, "status", "message"); found {
		status.Message = message
	}
	
	// Extract pod name
	if podName, found, _ := unstructured.NestedString(k8sSession.Object, "status", "podName"); found {
		status.PodName = podName
	}
	
	// Extract IPs
	if podIP, found, _ := unstructured.NestedString(k8sSession.Object, "status", "podIP"); found {
		status.PodIP = podIP
	}
	
	if vmIP, found, _ := unstructured.NestedString(k8sSession.Object, "status", "vmIP"); found {
		status.VMIP = vmIP
	}
	
	// Extract endpoints
	if endpoints, found, _ := unstructured.NestedMap(k8sSession.Object, "status", "endpoints"); found {
		status.Endpoints = make(map[string]string)
		for key, value := range endpoints {
			if strVal, ok := value.(string); ok {
				status.Endpoints[key] = strVal
			}
		}
	}
	
	return status
}

func (h *LabSessionHandler) syncSessionStatuses(sessions []models.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := range sessions {
		if isActiveSessionStatus(sessions[i].Status) {
			if k8sSession, err := h.k8sClient.Resource(h.labSessionGVR).Namespace("default").Get(ctx, fmt.Sprintf("session-%s", sessions[i].ID.String()), metav1.GetOptions{}); err == nil {
				// Update session status based on K8s status
				status, found, _ := unstructured.NestedString(k8sSession.Object, "status", "phase")
				if found && status != "" {
					dbStatus := convertPhaseToDBStatus(status)
					if dbStatus != "" && dbStatus != sessions[i].Status {
						h.db.DB.Model(&sessions[i]).Update("status", dbStatus)
						sessions[i].Status = dbStatus
					}
				}
			}
		}
	}
}

// Session statuses allowed by the sessions_status_check constraint
// (migrations/001_initial_schema.up.sql and models.Session).
const (
	SessionStatusPending   = "pending"
	SessionStatusRunning   = "running"
	SessionStatusCompleted = "completed"
	SessionStatusFailed    = "failed"
	SessionStatusExpired   = "expired"
)

// isActiveSessionStatus reports whether the session may still have Kubernetes resources
func isActiveSessionStatus(status string) bool {
	return status == SessionStatusPending || status == SessionStatusRunning
}

// needsK8sDelete reports whether deleting the session must also delete its
// LabSession. Failed and expired sessions keep their pod, PVCs and Service
// until the LabSession is deleted; completed means the API already deleted it.
func needsK8sDelete(status string) bool {
	return status != SessionStatusCompleted
}

// convertPhaseToDBStatus maps a LabSession phase to a session status. It returns
// "" for phases with no DB equivalent (e.g. Terminating), which leave the row unchanged.
func convertPhaseToDBStatus(phase string) string {
	switch phase {
	case "Pending", "Creating":
		return SessionStatusPending
	case "Running":
		return SessionStatusRunning
	case "Failed":
		return SessionStatusFailed
	case "Terminated":
		return SessionStatusCompleted
	default:
		return ""
	}
}

func generatePassword() string {
	return fmt.Sprintf("lab-%s", uuid.New().String()[:8])
}

// Response types for Swagger documentation

type CreateLabSessionResponse struct {
	Message     string      `json:"message"`
	Session     interface{} `json:"session"`
	OperationID string      `json:"operation_id"`
	Credentials interface{} `json:"credentials"`
}

type LabSessionResponse struct {
	Session   interface{}      `json:"session"`
	K8sStatus *K8sSessionStatus `json:"k8s_status,omitempty"`
}

type ListLabSessionsResponse struct {
	Sessions []interface{} `json:"sessions"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}