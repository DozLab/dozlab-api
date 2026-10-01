package unit

import (
	"bytes"
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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A database with users and labs tables and no rows: a request that gets past the role check
// then fails on "lab not found" (sessions) or reaches the insert (labs).
func setupRoleCheckDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Lab{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return &database.Database{DB: db}
}

// Calls the handler the way the router does after AuthMiddleware: user_id set, and role set
// unless it is "" (a token without a role).
func callWithRole(handler gin.HandlerFunc, role, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", uuid.New())
	if role != "" {
		c.Set("role", role)
	}
	handler(c)
	return w
}

func TestCreateLabSession_OnlyInstructorsAndAdminsSetOptions(t *testing.T) {
	handler := handlers.NewLabSessionHandler(setupRoleCheckDB(t), nil)
	labID := uuid.New().String() // no such lab: past the role check, the answer is 404

	noOptions := `{"lab_id":"` + labID + `"}`
	passwordOnly := `{"lab_id":"` + labID + `","config":{"vscode_password":"my-own-password"}}`
	options := map[string]string{
		"timeout":                `{"lab_id":"` + labID + `","timeout":"8h"}`,
		"config.enable_terminal": `{"lab_id":"` + labID + `","config":{"enable_terminal":true}}`,
		"config.enable_vscode":   `{"lab_id":"` + labID + `","config":{"enable_vscode":false}}`,
		"config.enable_ssh":      `{"lab_id":"` + labID + `","config":{"enable_ssh":false}}`,
	}

	tests := []struct {
		name       string
		role       string
		body       string
		wantStatus int
		wantOption string // named in the 403 body
	}{
		{"student without options", "student", noOptions, http.StatusNotFound, ""},
		{"student choosing a VS Code password", "student", passwordOnly, http.StatusNotFound, ""},
		{"student setting timeout", "student", options["timeout"], http.StatusForbidden, "timeout"},
		{"student setting enable_terminal", "student", options["config.enable_terminal"], http.StatusForbidden, "config.enable_terminal"},
		{"student setting enable_vscode to false", "student", options["config.enable_vscode"], http.StatusForbidden, "config.enable_vscode"},
		{"student setting enable_ssh to false", "student", options["config.enable_ssh"], http.StatusForbidden, "config.enable_ssh"},
		{"no role in the token, with an option", "", options["timeout"], http.StatusForbidden, "timeout"},
		{"no role in the token, without options", "", noOptions, http.StatusNotFound, ""},
		{"unknown role with an option", "tutor", options["timeout"], http.StatusForbidden, "timeout"},
		{"instructor without options", "instructor", noOptions, http.StatusNotFound, ""},
		{"instructor setting timeout", "instructor", options["timeout"], http.StatusNotFound, ""},
		{"instructor setting enable_ssh", "instructor", options["config.enable_ssh"], http.StatusNotFound, ""},
		{"admin without options", "admin", noOptions, http.StatusNotFound, ""},
		{"admin setting timeout", "admin", options["timeout"], http.StatusNotFound, ""},
		{"admin setting enable_vscode", "admin", options["config.enable_vscode"], http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := callWithRole(handler.CreateLabSession, tt.role, tt.body)
			assert.Equal(t, tt.wantStatus, w.Code, w.Body.String())
			if tt.wantStatus == http.StatusForbidden {
				var resp struct {
					Error   string   `json:"error"`
					Options []string `json:"options"`
				}
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, "Only instructors and admins can set session options", resp.Error)
				assert.Equal(t, []string{tt.wantOption}, resp.Options)
			}
		})
	}
}

func TestCreateLab_OnlyInstructorsAndAdmins(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{"student", "student", http.StatusForbidden},
		{"no role in the token", "", http.StatusForbidden},
		{"unknown role", "tutor", http.StatusForbidden},
		{"instructor", "instructor", http.StatusCreated},
		{"admin", "admin", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupRoleCheckDB(t)
			handler := handlers.NewLabHandler(db)
			w := callWithRole(handler.CreateLab, tt.role, `{"name":"Role check lab","slug":"role-check-lab"}`)
			assert.Equal(t, tt.wantStatus, w.Code, w.Body.String())

			var labs int64
			db.DB.Model(&models.Lab{}).Count(&labs)
			if tt.wantStatus == http.StatusCreated {
				assert.Equal(t, int64(1), labs)
			} else {
				assert.Equal(t, int64(0), labs, "a refused request must not create a lab")
				assert.Contains(t, w.Body.String(), "Only instructors and admins can create labs")
			}
		})
	}
}
