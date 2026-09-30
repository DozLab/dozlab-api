package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCORSRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://dozlab.github.io", "http://localhost:3000/"}))
	r.GET("/api/v1/labs/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestCORS(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		origin      string
		preflight   bool
		wantStatus  int
		wantAllowed string
	}{
		{"allowed origin", http.MethodGet, "https://dozlab.github.io", false, http.StatusOK, "https://dozlab.github.io"},
		{"allowed origin, trailing slash in config", http.MethodGet, "http://localhost:3000", false, http.StatusOK, "http://localhost:3000"},
		{"other origin", http.MethodGet, "https://evil.example", false, http.StatusOK, ""},
		{"no origin", http.MethodGet, "", false, http.StatusOK, ""},
		{"preflight from allowed origin", http.MethodOptions, "https://dozlab.github.io", true, http.StatusNoContent, "https://dozlab.github.io"},
		{"preflight from other origin", http.MethodOptions, "https://evil.example", true, http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/labs/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.preflight {
				req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			w := httptest.NewRecorder()
			newCORSRouter().ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.wantAllowed {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantAllowed)
			}
			if tt.preflight && tt.wantAllowed != "" && w.Header().Get("Access-Control-Allow-Headers") == "" {
				t.Error("preflight response has no Access-Control-Allow-Headers")
			}
		})
	}
}
