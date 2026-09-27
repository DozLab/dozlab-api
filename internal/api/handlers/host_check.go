package handlers

import (
	"net/http"

	"dozlab-backend/internal/hostcheck"

	"github.com/gin-gonic/gin"
)

// HostCheckHandler checks hosts against lab session resource requests
type HostCheckHandler struct{}

// NewHostCheckHandler creates a new host check handler
func NewHostCheckHandler() *HostCheckHandler {
	return &HostCheckHandler{}
}

// HostCheckRequest is the body of POST /api/v1/host-check
type HostCheckRequest struct {
	Requests hostcheck.Requests `json:"requests"`
	Host     hostcheck.Host     `json:"host"`
	// Sessions defaults to 1
	Sessions *int `json:"sessions"`
	// RequireKVM defaults to true
	RequireKVM *bool `json:"require_kvm"`
	// Headroom is the fraction of the host reserved for the OS/kubelet; defaults to 0.2
	Headroom *float64 `json:"headroom"`
}

// CheckHost validates a host against LabSession resource requests
// @Summary Check host capacity
// @Description Check whether a host can run N lab sessions (cpu/memory/storage/KVM)
// @Tags host-check
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body HostCheckRequest true "Requests, host and options"
// @Success 200 {object} hostcheck.Result "Check result; ok is false when the host falls short"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Router /api/v1/host-check [post]
func (h *HostCheckHandler) CheckHost(c *gin.Context) {
	var req HostCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data: " + err.Error(),
		})
		return
	}

	opts := hostcheck.Options{Sessions: 1, RequireKVM: true, Headroom: hostcheck.DefaultHeadroom}
	if req.Sessions != nil {
		opts.Sessions = *req.Sessions
	}
	if req.RequireKVM != nil {
		opts.RequireKVM = *req.RequireKVM
	}
	if req.Headroom != nil {
		opts.Headroom = *req.Headroom
	}

	result, err := hostcheck.Evaluate(req.Requests, req.Host, opts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}
