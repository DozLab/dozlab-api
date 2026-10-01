package handlers

import (
	"net/http"
	"strconv"

	"dozlab-backend/internal/audit"
	"dozlab-backend/internal/authz"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"dozlab-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LabHandler struct {
	db         *database.Database
	labService services.LabService
}

func NewLabHandler(db *database.Database) *LabHandler {
	return &LabHandler{
		db:         db,
		labService: services.NewLabService(db),
	}
}

func (h *LabHandler) GetLabs(c *gin.Context) {
	page := 1
	limit := 20
	
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	category := c.Query("category")
	
	labs, total, err := h.labService.GetLabs(c.Request.Context(), page, limit, category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch labs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"labs": labs,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": total,
		},
	})
}

func (h *LabHandler) CreateLab(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	// Only instructors and admins create labs (docs/decision.md, owner decision 2026-09-30)
	if !can(c, authz.LabsCreate) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only instructors and admins can create labs",
		})
		return
	}

	var lab models.Lab
	if err := c.ShouldBindJSON(&lab); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Set creator
	userUUID := userID.(uuid.UUID)
	lab.CreatedBy = &userUUID

	if err := h.db.DB.Create(&lab).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create lab",
		})
		return
	}

	audit.SetResource(c, "labs", lab.ID.String())
	audit.Annotate(c).New = map[string]interface{}{"name": lab.Name, "slug": lab.Slug}

	// Load the creator relationship
	h.db.DB.Preload("Creator").First(&lab, lab.ID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Lab created successfully",
		"lab":     lab,
	})
}

func (h *LabHandler) GetLab(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	var lab models.Lab
	if err := h.db.DB.Preload("Creator").Preload("LabSpecs").First(&lab, "id = ?", labID).Error; err != nil {
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

	// Check if user can access this lab
	if !can(c, authz.LabsReadUnpublished) && !lab.IsPublished {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Lab is not published",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lab": lab,
	})
}

func (h *LabHandler) UpdateLab(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
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

	var lab models.Lab
	if err := h.db.DB.First(&lab, "id = ?", labID).Error; err != nil {
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

	// Check if user can update this lab
	if !can(c, authz.LabsManageAny) && (lab.CreatedBy == nil || *lab.CreatedBy != userID.(uuid.UUID)) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "You don't have permission to update this lab",
		})
		return
	}

	type UpdateLabRequest struct {
		Name              *string  `json:"name,omitempty"`
		Description       *string  `json:"description,omitempty"`
		DifficultyLevel   *string  `json:"difficulty_level,omitempty"`
		EstimatedDuration *int     `json:"estimated_duration,omitempty"`
		Category          *string  `json:"category,omitempty"`
		Tags              []string `json:"tags,omitempty"`
		IsPublished       *bool    `json:"is_published,omitempty"`
		// VM size for the lab's sessions; same limits as models.Lab
		VMVCPUs     *int `json:"vm_vcpus,omitempty" binding:"omitempty,min=1,max=8"`
		VMMemoryMiB *int `json:"vm_memory_mib,omitempty" binding:"omitempty,min=256,max=16384"`
		VMDiskGiB   *int `json:"vm_disk_gib,omitempty" binding:"omitempty,min=1,max=100"`
	}

	var req UpdateLabRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Update fields if provided
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.DifficultyLevel != nil {
		updates["difficulty_level"] = *req.DifficultyLevel
	}
	if req.EstimatedDuration != nil {
		updates["estimated_duration"] = *req.EstimatedDuration
	}
	if req.Category != nil {
		updates["category"] = *req.Category
	}
	if req.Tags != nil {
		updates["tags"] = req.Tags
	}
	if req.IsPublished != nil {
		updates["is_published"] = *req.IsPublished
	}
	if req.VMVCPUs != nil {
		updates["vm_vcpus"] = *req.VMVCPUs
	}
	if req.VMMemoryMiB != nil {
		updates["vm_memory_mib"] = *req.VMMemoryMiB
	}
	if req.VMDiskGiB != nil {
		updates["vm_disk_gib"] = *req.VMDiskGiB
	}

	if len(updates) > 0 {
		if err := h.db.DB.Model(&lab).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to update lab",
			})
			return
		}
	}

	// Fetch updated lab
	h.db.DB.Preload("Creator").First(&lab, "id = ?", labID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Lab updated successfully",
		"lab":     lab,
	})
}

func (h *LabHandler) DeleteLab(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
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

	var lab models.Lab
	if err := h.db.DB.First(&lab, "id = ?", labID).Error; err != nil {
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

	// Check if user can delete this lab
	if !can(c, authz.LabsManageAny) && (lab.CreatedBy == nil || *lab.CreatedBy != userID.(uuid.UUID)) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "You don't have permission to delete this lab",
		})
		return
	}

	if err := h.db.DB.Delete(&lab).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete lab",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Lab deleted successfully",
	})
}

// Lab Specs handlers with composite key support

func (h *LabHandler) GetLabSpecs(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	var specs []models.LabSpec
	if err := h.db.DB.Where("lab_id = ?", labID).Order("version DESC").Find(&specs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch lab specifications",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lab_specs": specs,
	})
}

func (h *LabHandler) CreateLabSpec(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	var spec models.LabSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Set lab ID and find next version
	spec.LabID = labID

	// Get the latest version
	var latestSpec models.LabSpec
	if err := h.db.DB.Where("lab_id = ?", labID).Order("version DESC").First(&latestSpec).Error; err == nil {
		spec.Version = latestSpec.Version + 1
	} else {
		spec.Version = 1
	}

	if err := h.db.DB.Create(&spec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create lab specification",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Lab specification created successfully",
		"lab_spec": spec,
	})
}

func (h *LabHandler) GetLabSpec(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	versionStr := c.Param("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid version number",
		})
		return
	}

	var spec models.LabSpec
	if err := h.db.DB.Where("lab_id = ? AND version = ?", labID, version).First(&spec).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Lab specification not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lab_spec": spec,
	})
}

func (h *LabHandler) UpdateLabSpec(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	versionStr := c.Param("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid version number",
		})
		return
	}

	var spec models.LabSpec
	if err := h.db.DB.Where("lab_id = ? AND version = ?", labID, version).First(&spec).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Lab specification not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database error",
		})
		return
	}

	type UpdateLabSpecRequest struct {
		Specification      interface{} `json:"specification,omitempty"`
		KubernetesManifest interface{} `json:"kubernetes_manifest,omitempty"`
		ValidationRules    interface{} `json:"validation_rules,omitempty"`
		IsActive           *bool       `json:"is_active,omitempty"`
	}

	var req UpdateLabSpecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Update fields if provided
	updates := make(map[string]interface{})
	if req.Specification != nil {
		updates["specification"] = req.Specification
	}
	if req.KubernetesManifest != nil {
		updates["kubernetes_manifest"] = req.KubernetesManifest
	}
	if req.ValidationRules != nil {
		updates["validation_rules"] = req.ValidationRules
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) > 0 {
		if err := h.db.DB.Model(&spec).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to update lab specification",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Lab specification updated successfully",
		"lab_spec": spec,
	})
}

func (h *LabHandler) DeleteLabSpec(c *gin.Context) {
	labIDStr := c.Param("labId")
	labID, err := uuid.Parse(labIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}

	versionStr := c.Param("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid version number",
		})
		return
	}

	if err := h.db.DB.Where("lab_id = ? AND version = ?", labID, version).Delete(&models.LabSpec{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete lab specification",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Lab specification deleted successfully",
	})
}

// Session handlers

func (h *LabHandler) GetSessions(c *gin.Context) {
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
	if !can(c, authz.SessionsManageAny) {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch sessions",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
	})
}

func (h *LabHandler) CreateSession(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}

	type CreateSessionRequest struct {
		LabID uuid.UUID `json:"lab_id" binding:"required"`
	}

	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	session := models.Session{
		UserID: userID.(uuid.UUID),
		LabID:  req.LabID,
		Status: "pending",
	}

	if err := h.db.DB.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create session",
		})
		return
	}

	// Load relationships
	h.db.DB.Preload("Lab").Preload("User").First(&session, session.ID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Session created successfully",
		"session": session,
	})
}

func (h *LabHandler) GetSession(c *gin.Context) {
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

	var session models.Session
	query := h.db.DB.Preload("Lab").Preload("User")

	// Non-admin users can only access their own sessions
	if !can(c, authz.SessionsManageAny) {
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

	c.JSON(http.StatusOK, gin.H{
		"session": session,
	})
}

func (h *LabHandler) UpdateSessionStatus(c *gin.Context) {
	sessionIDStr := c.Param("id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid session ID",
		})
		return
	}

	type UpdateStatusRequest struct {
		Status string `json:"status" binding:"required,oneof=pending running completed failed expired"`
	}

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	if err := h.db.DB.Model(&models.Session{}).Where("id = ?", sessionID).Update("status", req.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update session status",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session status updated successfully",
	})
}

func (h *LabHandler) DeleteSession(c *gin.Context) {
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

	query := h.db.DB

	// Non-admin users can only delete their own sessions
	if !can(c, authz.SessionsManageAny) {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Where("id = ?", sessionID).Delete(&models.Session{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete session",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session deleted successfully",
	})
}