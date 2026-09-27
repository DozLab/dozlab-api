package unit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"dozlab-backend/internal/api/handlers"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Note: Using real database.Database with in-memory SQLite for testing

func setupTestDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	
	// Auto migrate the schema
	db.AutoMigrate(&models.User{})
	
	return db
}

func setupAuthHandler() *handlers.AuthHandler {
	db := setupTestDB()
	mockDB := &database.Database{DB: db}
	return handlers.NewAuthHandler(mockDB)
}

func TestAuthHandler_Register(t *testing.T) {
	// Set JWT secret for testing
	os.Setenv("JWT_SECRET", "test-secret-key-32-characters-long")
	defer os.Unsetenv("JWT_SECRET")

	gin.SetMode(gin.TestMode)
	handler := setupAuthHandler()

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		expectToken    bool
	}{
		{
			name: "successful registration",
			requestBody: map[string]interface{}{
				"username":   "testuser",
				"email":      "test@example.com",
				"password":   "StrongPassword123!",
				"first_name": "Test",
				"last_name":  "User",
			},
			expectedStatus: http.StatusCreated,
			expectToken:    true,
		},
		{
			name: "invalid password",
			requestBody: map[string]interface{}{
				"username":   "testuser2",
				"email":      "test2@example.com",
				"password":   "weak",
				"first_name": "Test",
				"last_name":  "User",
			},
			expectedStatus: http.StatusBadRequest,
			expectToken:    false,
		},
		{
			name: "missing required fields",
			requestBody: map[string]interface{}{
				"username": "testuser3",
			},
			expectedStatus: http.StatusBadRequest,
			expectToken:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			jsonBody, _ := json.Marshal(tt.requestBody)
			req, _ := http.NewRequest("POST", "/auth/register", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			// Call handler
			handler.Register(c)

			// Assertions
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectToken {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Contains(t, response, "tokens")
				assert.Contains(t, response, "user")
			}
		})
	}
}

func TestAuthHandler_Login(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key-32-characters-long")
	defer os.Unsetenv("JWT_SECRET")

	gin.SetMode(gin.TestMode)
	handler := setupAuthHandler()

	// Create a test user first
	testUser := models.User{
		Username:     "testlogin",
		Email:        "login@example.com",
		PasswordHash: "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi", // "password"
		FirstName:    strPtr("Test"),
		LastName:     strPtr("Login"),
		Role:         "student",
		IsActive:     true,
	}
	// Access the database through the handler's db field
	handlerDB := setupTestDB()
	mockDB := &database.Database{DB: handlerDB}
	handler = handlers.NewAuthHandler(mockDB)
	mockDB.DB.Create(&testUser)

	tests := []struct {
		name           string
		username       string
		password       string
		expectedStatus int
		expectToken    bool
	}{
		{
			name:           "successful login",
			username:       "testlogin",
			password:       "password",
			expectedStatus: http.StatusOK,
			expectToken:    true,
		},
		{
			name:           "invalid credentials",
			username:       "testlogin",
			password:       "wrongpassword",
			expectedStatus: http.StatusUnauthorized,
			expectToken:    false,
		},
		{
			name:           "user not found",
			username:       "nonexistent",
			password:       "password",
			expectedStatus: http.StatusUnauthorized,
			expectToken:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loginReq := map[string]string{
				"username": tt.username,
				"password": tt.password,
			}
			
			jsonBody, _ := json.Marshal(loginReq)
			req, _ := http.NewRequest("POST", "/auth/login", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			handler.Login(c)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectToken {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Contains(t, response, "tokens")
			}
		})
	}
}

func TestAuthHandler_RefreshToken(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key-32-characters-long")
	defer os.Unsetenv("JWT_SECRET")

	gin.SetMode(gin.TestMode)
	handler := setupAuthHandler()

	// This would require a valid refresh token setup
	// For now, testing the basic structure
	
	refreshReq := map[string]string{
		"refresh_token": "invalid-token",
	}
	
	jsonBody, _ := json.Marshal(refreshReq)
	req, _ := http.NewRequest("POST", "/auth/refresh", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.RefreshToken(c)

	// Should return unauthorized for invalid token
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
func strPtr(s string) *string { return &s }
