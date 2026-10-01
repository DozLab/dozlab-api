package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dozlab-backend/internal/api/handlers"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// A LabSession as the API creates it and the controller reports on it. usage nil: the
// controller hasn't written status.usage.
func labSessionObject(session models.Session, phase string, usage map[string]interface{}) *unstructured.Unstructured {
	status := map[string]interface{}{"phase": phase, "message": "test"}
	if usage != nil {
		status["usage"] = usage
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "dozlab.io/v1",
		"kind":       "LabSession",
		"metadata": map[string]interface{}{
			"name":      "session-" + session.ID.String(),
			"namespace": "default",
			"labels": map[string]interface{}{
				"user-id":    session.UserID.String(),
				"session-id": session.ID.String(),
			},
		},
		"status": status,
	}}
}

type usageEnv struct {
	handler                *handlers.LabSessionHandler
	alice, bob             uuid.UUID
	docker, k8s            models.Lab
	running, failed, fresh models.Session // alice's
	bobs                   models.Session
}

func newUsageEnv(t *testing.T) *usageEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, gormDB.AutoMigrate(&models.User{}, &models.Lab{}, &models.Session{}))

	e := &usageEnv{alice: uuid.New(), bob: uuid.New()}
	for id, name := range map[uuid.UUID]string{e.alice: "alice", e.bob: "bob"} {
		require.NoError(t, gormDB.Create(&models.User{ID: id, Username: name, Email: name + "@example.com", PasswordHash: "x", Role: "instructor", IsActive: true}).Error)
	}
	e.docker = models.Lab{ID: uuid.New(), Name: "Docker basics", Slug: "docker-basics", IsPublished: true}
	e.k8s = models.Lab{ID: uuid.New(), Name: "Kubernetes", Slug: "kubernetes", IsPublished: true}
	require.NoError(t, gormDB.Create(&e.docker).Error)
	require.NoError(t, gormDB.Create(&e.k8s).Error)

	session := func(user uuid.UUID, lab models.Lab, status string) models.Session {
		s := models.Session{ID: uuid.New(), UserID: user, LabID: lab.ID, Status: status}
		require.NoError(t, gormDB.Create(&s).Error)
		return s
	}
	e.running = session(e.alice, e.docker, "running")
	e.failed = session(e.alice, e.docker, "failed")
	e.fresh = session(e.alice, e.k8s, "pending")
	e.bobs = session(e.bob, e.k8s, "running")

	gvr := schema.GroupVersionResource{Group: "dozlab.io", Version: "v1", Resource: "labsessions"}
	k8sClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "LabSessionList"},
		// a running VM of the default size
		labSessionObject(e.running, "Running", map[string]interface{}{"running": true, "cpuRequest": "850m", "memoryRequest": "1920Mi", "storage": "6Gi"}),
		// a failed VM whose pod is gone: it only keeps its storage
		labSessionObject(e.failed, "Failed", map[string]interface{}{"running": false, "storage": "6Gi"}),
		// a VM the controller hasn't reported on yet
		labSessionObject(e.fresh, "Creating", nil),
		// someone else's, larger
		labSessionObject(e.bobs, "Running", map[string]interface{}{"running": true, "cpuRequest": "850m", "memoryRequest": "3456Mi", "storage": "9Gi"}),
	)
	e.handler = handlers.NewLabSessionHandler(&database.Database{DB: gormDB, SqlDB: sqlDB}, k8sClient)
	return e
}

type usageTotal struct {
	VMs           int `json:"vms"`
	RunningVMs    int `json:"running_vms"`
	CPUMillicores int `json:"cpu_millicores"`
	MemoryMiB     int `json:"memory_mib"`
	StorageMiB    int `json:"storage_mib"`
}

type usageResponse struct {
	Labs []struct {
		LabID   string `json:"lab_id"`
		LabName string `json:"lab_name"`
		VMs     []struct {
			SessionID string `json:"session_id"`
			UserID    string `json:"user_id"`
			Status    string `json:"status"`
			Phase     string `json:"phase"`
			Usage     *struct {
				Running       bool `json:"running"`
				CPUMillicores int  `json:"cpu_millicores"`
				MemoryMiB     int  `json:"memory_mib"`
				StorageMiB    int  `json:"storage_mib"`
			} `json:"usage"`
		} `json:"vms"`
		Total usageTotal `json:"total"`
	} `json:"labs"`
	Total usageTotal `json:"total"`
}

func getUsage(t *testing.T, e *usageEnv, user uuid.UUID, role string) usageResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/lab-sessions/usage", nil)
	c.Set("user_id", user)
	c.Set("role", role)
	e.handler.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp usageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestGetUsage_OwnVMsPerLab(t *testing.T) {
	e := newUsageEnv(t)
	resp := getUsage(t, e, e.alice, "instructor")

	// Labs by name; only Alice's VMs
	require.Len(t, resp.Labs, 2)
	docker, k8s := resp.Labs[0], resp.Labs[1]
	assert.Equal(t, "Docker basics", docker.LabName)
	assert.Equal(t, e.docker.ID.String(), docker.LabID)
	assert.Equal(t, "Kubernetes", k8s.LabName)

	// Docker basics: a running VM and a failed one that only keeps its storage
	require.Len(t, docker.VMs, 2)
	byID := map[string]int{docker.VMs[0].SessionID: 0, docker.VMs[1].SessionID: 1}
	running := docker.VMs[byID[e.running.ID.String()]]
	require.NotNil(t, running.Usage)
	assert.True(t, running.Usage.Running)
	assert.Equal(t, 850, running.Usage.CPUMillicores)
	assert.Equal(t, 1920, running.Usage.MemoryMiB)
	assert.Equal(t, 6144, running.Usage.StorageMiB)
	assert.Equal(t, "Running", running.Phase)
	assert.Equal(t, "running", running.Status)
	failed := docker.VMs[byID[e.failed.ID.String()]]
	require.NotNil(t, failed.Usage)
	assert.False(t, failed.Usage.Running)
	assert.Equal(t, 0, failed.Usage.CPUMillicores)
	assert.Equal(t, 6144, failed.Usage.StorageMiB)
	// CPU and memory of the running VM only; storage of both
	assert.Equal(t, usageTotal{VMs: 2, RunningVMs: 1, CPUMillicores: 850, MemoryMiB: 1920, StorageMiB: 12288}, docker.Total)

	// Kubernetes: one VM the controller hasn't reported on
	require.Len(t, k8s.VMs, 1)
	assert.Equal(t, e.fresh.ID.String(), k8s.VMs[0].SessionID)
	assert.Nil(t, k8s.VMs[0].Usage)
	assert.Equal(t, usageTotal{VMs: 1}, k8s.Total)

	assert.Equal(t, usageTotal{VMs: 3, RunningVMs: 1, CPUMillicores: 850, MemoryMiB: 1920, StorageMiB: 12288}, resp.Total)

	// Bob's VM is nowhere in Alice's report
	for _, lab := range resp.Labs {
		for _, vm := range lab.VMs {
			assert.NotEqual(t, e.bobs.ID.String(), vm.SessionID)
			assert.Equal(t, e.alice.String(), vm.UserID)
		}
	}
}

func TestGetUsage_OthersAndAdmin(t *testing.T) {
	e := newUsageEnv(t)

	bob := getUsage(t, e, e.bob, "instructor")
	require.Len(t, bob.Labs, 1)
	assert.Equal(t, usageTotal{VMs: 1, RunningVMs: 1, CPUMillicores: 850, MemoryMiB: 3456, StorageMiB: 9216}, bob.Total)

	// Someone with no VMs
	nobody := getUsage(t, e, uuid.New(), "instructor")
	assert.Len(t, nobody.Labs, 0)
	assert.Equal(t, usageTotal{}, nobody.Total)

	// An admin sees everyone's
	admin := getUsage(t, e, uuid.New(), "admin")
	require.Len(t, admin.Labs, 2)
	assert.Equal(t, usageTotal{VMs: 4, RunningVMs: 2, CPUMillicores: 1700, MemoryMiB: 5376, StorageMiB: 21504}, admin.Total)
	assert.Equal(t, usageTotal{VMs: 2, RunningVMs: 1, CPUMillicores: 850, MemoryMiB: 3456, StorageMiB: 9216}, admin.Labs[1].Total)
}

// GET /lab-sessions/{id} carries the same usage for one VM.
func TestGetLabSession_IncludesUsage(t *testing.T) {
	e := newUsageEnv(t)
	get := func(session models.Session) map[string]interface{} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Params = gin.Params{{Key: "id", Value: session.ID.String()}}
		c.Set("user_id", e.alice)
		c.Set("role", "instructor")
		e.handler.GetLabSession(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			K8sStatus map[string]interface{} `json:"k8s_status"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.K8sStatus
	}

	status := get(e.running)
	assert.Equal(t, map[string]interface{}{"running": true, "cpu_millicores": float64(850), "memory_mib": float64(1920), "storage_mib": float64(6144)}, status["usage"])

	// Not reported yet: no usage key rather than zeros
	_, has := get(e.fresh)["usage"]
	assert.False(t, has)
}
