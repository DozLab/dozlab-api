package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"dozlab-backend/internal/api"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type APITestSuite struct {
	suite.Suite
	router *gin.Engine
	db     *database.Database
}

func (suite *APITestSuite) SetupSuite() {
	// Set test environment variables
	os.Setenv("JWT_SECRET", "test-jwt-secret-key-32-characters-long")
	os.Setenv("ENV", "test")
	
	gin.SetMode(gin.TestMode)
	
	// Setup test database
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	suite.NoError(err)
	
	// Auto migrate the schema
	err = gormDB.AutoMigrate(&models.User{}, &models.Lab{}, &models.LabSpec{})
	suite.NoError(err)
	
	sqlDB, err := gormDB.DB()
	suite.NoError(err)

	suite.db = &database.Database{DB: gormDB, SqlDB: sqlDB}
	
	// Setup router
	suite.router = gin.New()
	api.SetupRoutes(suite.router, suite.db)
}

func (suite *APITestSuite) TearDownSuite() {
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("ENV")
}

func (suite *APITestSuite) SetupTest() {
	// Clean database before each test
	suite.db.DB.Exec("DELETE FROM users")
	suite.db.DB.Exec("DELETE FROM labs")
	suite.db.DB.Exec("DELETE FROM lab_specs")
}

func (suite *APITestSuite) TestHealthCheck() {
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "ok", response["status"])
}

func (suite *APITestSuite) TestUserRegistrationFlow() {
	// Test user registration
	registerData := map[string]string{
		"username":   "integrationtest",
		"email":      "integration@test.com",
		"password":   "StrongPassword123!",
		"first_name": "Integration",
		"last_name":  "Test",
	}
	
	jsonBody, _ := json.Marshal(registerData)
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusCreated, w.Code)
	
	var registerResponse map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &registerResponse)
	assert.NoError(suite.T(), err)
	assert.Contains(suite.T(), registerResponse, "tokens")
	assert.Contains(suite.T(), registerResponse, "user")
	
	tokens := registerResponse["tokens"].(map[string]interface{})
	accessToken := tokens["access_token"].(string)
	assert.NotEmpty(suite.T(), accessToken)
	
	// Test accessing protected endpoint with token
	req, _ = http.NewRequest("GET", "/api/v1/users/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusOK, w.Code)
	
	var profileResponse struct {
		User models.UserResponse `json:"user"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &profileResponse)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "integrationtest", profileResponse.User.Username)
}

func (suite *APITestSuite) TestLoginFlow() {
	// First create a user
	user := models.User{
		Username:     "logintest",
		Email:        "login@test.com",
		PasswordHash: "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi", // "password"
		FirstName:    strPtr("Login"),
		LastName:     strPtr("Test"),
		Role:         "student",
		IsActive:     true,
	}
	suite.db.DB.Create(&user)
	
	// Test login
	loginData := map[string]string{
		"username": "logintest",
		"password": "password",
	}
	
	jsonBody, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusOK, w.Code)
	
	var loginResponse map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &loginResponse)
	assert.NoError(suite.T(), err)
	assert.Contains(suite.T(), loginResponse, "tokens")
	
	tokens := loginResponse["tokens"].(map[string]interface{})
	accessToken := tokens["access_token"].(string)
	assert.NotEmpty(suite.T(), accessToken)
}

func (suite *APITestSuite) TestUnauthorizedAccess() {
	// Test accessing protected endpoint without token
	req, _ := http.NewRequest("GET", "/api/v1/users/profile", nil)
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusUnauthorized, w.Code)
}

func (suite *APITestSuite) TestInvalidToken() {
	// Test accessing protected endpoint with invalid token
	req, _ := http.NewRequest("GET", "/api/v1/users/profile", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusUnauthorized, w.Code)
}

func (suite *APITestSuite) TestUserProfileUpdate() {
	// Create and login user first
	user := models.User{
		Username:     "updatetest",
		Email:        "update@test.com",
		PasswordHash: "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi",
		FirstName:    strPtr("Update"),
		LastName:     strPtr("Test"),
		Role:         "student",
		IsActive:     true,
	}
	suite.db.DB.Create(&user)
	
	// Login to get token
	loginData := map[string]string{
		"username": "updatetest",
		"password": "password",
	}
	
	jsonBody, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	var loginResponse map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResponse)
	tokens := loginResponse["tokens"].(map[string]interface{})
	accessToken := tokens["access_token"].(string)
	
	// Test profile update
	updateData := map[string]string{
		"first_name": "Updated",
		"last_name":  "Name",
		"email":      "updated@test.com",
	}
	
	jsonBody, _ = json.Marshal(updateData)
	req, _ = http.NewRequest("PUT", "/api/v1/users/profile", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	assert.Equal(suite.T(), http.StatusOK, w.Code)
	
	// Verify the update by getting profile
	req, _ = http.NewRequest("GET", "/api/v1/users/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	var profileResponse struct {
		User models.UserResponse `json:"user"`
	}
	json.Unmarshal(w.Body.Bytes(), &profileResponse)
	assert.Equal(suite.T(), strPtr("Updated"), profileResponse.User.FirstName)
	assert.Equal(suite.T(), strPtr("Name"), profileResponse.User.LastName)
	assert.Equal(suite.T(), "updated@test.com", profileResponse.User.Email)
}

func (suite *APITestSuite) TestCORSHeaders() {
	req, _ := http.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	
	// Check for CORS headers (if implemented)
	// This would depend on your CORS middleware implementation
}

func TestAPITestSuite(t *testing.T) {
	suite.Run(t, new(APITestSuite))
}

func strPtr(s string) *string { return &s }
