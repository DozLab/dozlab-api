package handlers

import (
	"net/http"

	"dozlab-backend/internal/audit"
	"dozlab-backend/internal/authz"
	"dozlab-backend/internal/estimate"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetLabEstimate returns what one VM of the lab reserves and stores.
// @Summary Resource estimate for a lab's VM
// @Description What one VM of the lab reserves (CPU and memory per container and in total), the node devices it holds, and the storage it uses while running and after it stops. Instructors and admins.
// @Tags labs
// @Produce json
// @Param labId path string true "Lab ID"
// @Param persistence query string false "Session option; only none is available"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/labs/{labId}/estimate [get]
func (h *LabHandler) GetLabEstimate(c *gin.Context) {
	labID, err := uuid.Parse(c.Param("labId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid lab ID",
		})
		return
	}
	audit.SetResource(c, "labs", labID.String())

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
	// An unpublished lab is visible to its creator and to admins
	userID, _ := c.Get("user_id")
	isCreator := lab.CreatedBy != nil && *lab.CreatedBy == userID
	if !lab.IsPublished && !isCreator && !can(c, authz.LabsReadUnpublished) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Lab is not published",
		})
		return
	}

	result, err := estimate.For(estimate.VM{
		VCPUs:     lab.VMVCPUs,
		MemoryMiB: lab.VMMemoryMiB,
		DiskGiB:   lab.VMDiskGiB,
	}, c.Query("persistence"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lab_id":   lab.ID,
		"estimate": result,
	})
}
