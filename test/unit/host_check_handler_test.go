package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dozlab-backend/internal/api/handlers"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostCheckHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/host-check", handlers.NewHostCheckHandler().CheckHost)

	const fits = `"requests":{"cpu":"500m","memory":"2Gi","storage":"10Gi"},` +
		`"host":{"cpu_cores":8,"memory":"32Gi","free_storage":"200Gi","kvm":true}`
	const noKVM = `"requests":{"cpu":"500m","memory":"2Gi","storage":"10Gi"},` +
		`"host":{"cpu_cores":8,"memory":"32Gi","free_storage":"200Gi","kvm":false}`

	tests := []struct {
		name       string
		body       string
		wantStatus int
		check      func(t *testing.T, resp map[string]interface{})
	}{
		{
			name:       "fits",
			body:       "{" + fits + "}",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Equal(t, true, resp["ok"])
				assert.Equal(t, float64(12), resp["max_sessions"])
			},
		},
		{
			name:       "defaults require kvm",
			body:       "{" + noKVM + "}",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Equal(t, false, resp["ok"])
				kvm, ok := resp["kvm"].(map[string]interface{})
				require.True(t, ok, "kvm check missing")
				assert.Equal(t, false, kvm["pass"])
			},
		},
		{
			name:       "require_kvm false",
			body:       "{" + noKVM + `,"require_kvm":false}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Equal(t, true, resp["ok"])
				assert.NotContains(t, resp, "kvm")
			},
		},
		{
			name:       "sessions",
			body:       "{" + fits + `,"sessions":20}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Equal(t, false, resp["ok"])
				assert.Equal(t, float64(12), resp["max_sessions"])
			},
		},
		{
			name:       "bad quantity",
			body:       `{"requests":{"memory":"lots"},"host":{"cpu_cores":8}}`,
			wantStatus: http.StatusBadRequest,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Contains(t, resp["error"], "requests.memory")
			},
		},
		{name: "bad headroom", body: "{" + fits + `,"headroom":1.5}`, wantStatus: http.StatusBadRequest},
		{name: "zero sessions", body: "{" + fits + `,"sessions":0}`, wantStatus: http.StatusBadRequest},
		{name: "malformed json", body: `{"requests":`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/host-check", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code, w.Body.String())
			if tt.check != nil {
				var resp map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				tt.check(t, resp)
			}
		})
	}
}
