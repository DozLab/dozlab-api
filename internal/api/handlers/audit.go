package handlers

import (
	"net/http"
	"strconv"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditHandler serves the audit log. It can only read it: entries are written by
// audit.Middleware and never changed.
type AuditHandler struct {
	db *database.Database
}

func NewAuditHandler(db *database.Database) *AuditHandler {
	return &AuditHandler{db: db}
}

// ListAuditLogs returns audit entries, newest first.
// @Summary List audit log entries
// @Description Who did what, to which record, from where and when. Admins only.
// @Tags admin
// @Produce json
// @Param user_id query string false "Entries for this user"
// @Param action query string false "Exact action, e.g. users:update_role or auth:login"
// @Param resource_type query string false "Data type, e.g. labs, sessions, users"
// @Param resource_id query string false "ID of the record acted on"
// @Param outcome query string false "attempted, success, denied or failure"
// @Param request_id query string false "Both entries of one change"
// @Param ip_address query string false "Client address"
// @Param from query string false "Entries at or after this time (RFC 3339)"
// @Param to query string false "Entries before this time (RFC 3339)"
// @Param page query int false "Page, from 1"
// @Param limit query int false "Entries per page, up to 200 (default 50)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/admin/audit-logs [get]
func (h *AuditHandler) ListAuditLogs(c *gin.Context) {
	page, limit := 1, 50
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	query := h.db.DB.Model(&models.AuditLog{})

	if v := c.Query("user_id"); v != "" {
		userID, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user_id"})
			return
		}
		query = query.Where("user_id = ?", userID)
	}
	if v := c.Query("request_id"); v != "" {
		requestID, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request_id"})
			return
		}
		query = query.Where("request_id = ?", requestID)
	}
	for _, column := range []string{"action", "resource_type", "resource_id", "outcome", "ip_address"} {
		if v := c.Query(column); v != "" {
			query = query.Where(column+" = ?", v)
		}
	}
	if v := c.Query("from"); v != "" {
		from, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid from: use RFC 3339, e.g. 2026-10-01T00:00:00Z"})
			return
		}
		query = query.Where("created_at >= ?", from)
	}
	if v := c.Query("to"); v != "" {
		to, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid to: use RFC 3339, e.g. 2026-10-02T00:00:00Z"})
			return
		}
		query = query.Where("created_at < ?", to)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the audit log"})
		return
	}

	entries := []models.AuditLog{}
	if err := query.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&entries).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the audit log"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"audit_logs": entries,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": total,
		},
	})
}
