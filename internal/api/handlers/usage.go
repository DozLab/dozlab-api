package handlers

import (
	"context"
	"net/http"
	"sort"
	"time"

	"dozlab-backend/internal/authz"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// VMUsage is what one VM holds in the cluster, from the LabSession's status.usage, which
// dozlab-controller writes. CPU is in thousandths of a core, memory and storage in MiB, the
// same units as the estimate (internal/estimate). These are the amounts set aside for the VM,
// not what it consumes.
type VMUsage struct {
	// Running is true while the VM's pod exists and hasn't finished, so it holds its CPU and
	// memory. A failed session whose pod is still there counts as running.
	Running       bool `json:"running"`
	CPUMillicores int  `json:"cpu_millicores"`
	MemoryMiB     int  `json:"memory_mib"`
	// StorageMiB is held until the session is deleted, running or not.
	StorageMiB int `json:"storage_mib"`
}

// usageFromStatus reads status.usage. It returns nil when the controller hasn't reported it
// (an older controller, or a session that isn't running yet).
func usageFromStatus(k8sSession *unstructured.Unstructured) *VMUsage {
	raw, found, _ := unstructured.NestedMap(k8sSession.Object, "status", "usage")
	if !found {
		return nil
	}
	usage := &VMUsage{}
	usage.Running, _ = raw["running"].(bool)
	if s, ok := raw["cpuRequest"].(string); ok {
		if q, err := resource.ParseQuantity(s); err == nil {
			usage.CPUMillicores = int(q.MilliValue())
		}
	}
	if s, ok := raw["memoryRequest"].(string); ok {
		usage.MemoryMiB = mib(s)
	}
	if s, ok := raw["storage"].(string); ok {
		usage.StorageMiB = mib(s)
	}
	return usage
}

// mib turns a Kubernetes quantity into MiB, rounding up; 0 if it can't be read.
func mib(quantity string) int {
	q, err := resource.ParseQuantity(quantity)
	if err != nil {
		return 0
	}
	return int((q.Value() + (1<<20 - 1)) >> 20)
}

// UsageTotal sums VMs.
type UsageTotal struct {
	VMs        int `json:"vms"`
	RunningVMs int `json:"running_vms"`
	// CPU and memory held by the running VMs
	CPUMillicores int `json:"cpu_millicores"`
	MemoryMiB     int `json:"memory_mib"`
	// Storage held by all the VMs, running or not
	StorageMiB int `json:"storage_mib"`
}

func (t *UsageTotal) add(u *VMUsage) {
	t.VMs++
	if u == nil {
		return
	}
	if u.Running {
		t.RunningVMs++
		t.CPUMillicores += u.CPUMillicores
		t.MemoryMiB += u.MemoryMiB
	}
	t.StorageMiB += u.StorageMiB
}

// VMUsageEntry is one VM in the usage report.
type VMUsageEntry struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	// Status is the session's status in the database; Phase is the controller's.
	Status    string    `json:"status"`
	Phase     string    `json:"phase,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Usage is nil when the controller hasn't reported it yet
	Usage *VMUsage `json:"usage"`
}

// LabUsage is the VMs of one lab and their sum.
type LabUsage struct {
	LabID   uuid.UUID      `json:"lab_id"`
	LabName string         `json:"lab_name"`
	VMs     []VMUsageEntry `json:"vms"`
	Total   UsageTotal     `json:"total"`
}

// GetUsage reports what the caller's VMs hold in the cluster, per VM and summed per lab.
// @Summary Resource usage of one's VMs
// @Description What each of the caller's VMs holds in the cluster (running or not, CPU and memory reserved, storage), grouped and summed per lab. Admins see everyone's. Instructors and admins.
// @Tags lab-sessions
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/lab-sessions/usage [get]
func (h *LabSessionHandler) GetUsage(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID not found in context",
		})
		return
	}
	everyone := can(c, authz.SessionsManageAny)

	// The VMs that exist are the LabSessions in the cluster; one list call gets them all
	listOptions := metav1.ListOptions{}
	if !everyone {
		listOptions.LabelSelector = "user-id=" + userID.(uuid.UUID).String()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	list, err := h.k8sClient.Resource(h.labSessionGVR).Namespace("default").List(ctx, listOptions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to read lab sessions from the cluster",
			"details": err.Error(),
		})
		return
	}
	inCluster := map[string]*unstructured.Unstructured{}
	ids := []uuid.UUID{}
	for i := range list.Items {
		item := &list.Items[i]
		id, err := uuid.Parse(item.GetLabels()["session-id"])
		if err != nil {
			continue
		}
		inCluster[id.String()] = item
		ids = append(ids, id)
	}

	// Their lab and owner come from the database
	var sessions []models.Session
	if len(ids) > 0 {
		query := h.db.DB.Preload("Lab").Where("id IN ?", ids)
		if !everyone {
			query = query.Where("user_id = ?", userID)
		}
		if err := query.Order("created_at").Find(&sessions).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to fetch sessions",
			})
			return
		}
	}

	byLab := map[uuid.UUID]*LabUsage{}
	total := UsageTotal{}
	for _, session := range sessions {
		k8sSession := inCluster[session.ID.String()]
		usage := usageFromStatus(k8sSession)
		phase, _, _ := unstructured.NestedString(k8sSession.Object, "status", "phase")

		lab, ok := byLab[session.LabID]
		if !ok {
			lab = &LabUsage{LabID: session.LabID, LabName: session.Lab.Name, VMs: []VMUsageEntry{}}
			byLab[session.LabID] = lab
		}
		lab.VMs = append(lab.VMs, VMUsageEntry{
			SessionID: session.ID,
			UserID:    session.UserID,
			Status:    session.Status,
			Phase:     phase,
			CreatedAt: session.CreatedAt,
			Usage:     usage,
		})
		lab.Total.add(usage)
		total.add(usage)
	}

	labs := make([]*LabUsage, 0, len(byLab))
	for _, lab := range byLab {
		labs = append(labs, lab)
	}
	sort.Slice(labs, func(i, j int) bool { return labs[i].LabName < labs[j].LabName })

	c.JSON(http.StatusOK, gin.H{
		"labs":  labs,
		"total": total,
	})
}
