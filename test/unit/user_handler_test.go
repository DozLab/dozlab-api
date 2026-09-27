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

func setupUserHandler() (*handlers.UserHandler, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	
	if err := db.AutoMigrate(&models.User{}, &models.UserProgress{}); err != nil {
		panic("failed to migrate database: " + err.Error())
	}

	mockDB := &database.Database{DB: db}
	return handlers.NewUserHandler(mockDB), db
}

func TestUserHandler_GetProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupUserHandler()

	// Create a test user
	testUser := models.User{
		ID:        uuid.New(),
		Username:  "testprofile",
		Email:     "profile@example.com",
		FirstName: strPtr("Test"),
		LastName:  strPtr("Profile"),
		Role:      "student",
		IsActive:  true,
	}
	db.Create(&testUser)

	tests := []struct {
		name           string
		userID         interface{}
		expectedStatus int
		expectUser     bool
	}{
		{
			name:           "successful profile retrieval",
			userID:         testUser.ID,
			expectedStatus: http.StatusOK,
			expectUser:     true,
		},
		{
			name:           "user not found",
			userID:         uuid.New(),
			expectedStatus: http.StatusNotFound,
			expectUser:     false,
		},
		{
			name:           "missing user ID in context",
			userID:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectUser:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/users/profile", nil)
			
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			// Set user_id in context if provided
			if tt.userID != nil {
				c.Set("user_id", tt.userID)
			}

			handler.GetProfile(c)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectUser {
				var response struct {
					User models.UserResponse `json:"user"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, testUser.Username, response.User.Username)
				assert.Equal(t, testUser.Email, response.User.Email)
			}
		})
	}
}

func TestUserHandler_UpdateProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupUserHandler()

	// Create a test user
	testUser := models.User{
		ID:        uuid.New(),
		Username:  "testupdate",
		Email:     "update@example.com",
		FirstName: strPtr("Test"),
		LastName:  strPtr("Update"),
		Role:      "student",
		IsActive:  true,
	}
	db.Create(&testUser)

	// Test successful update
	updateData := map[string]string{
		"first_name": "Updated",
		"last_name":  "Name",
		"email":      "updated@example.com",
	}
	
	jsonBody, _ := json.Marshal(updateData)
	req, _ := http.NewRequest("PUT", "/users/profile", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_id", testUser.ID)

	handler.UpdateProfile(c)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify the update
	var updatedUser models.User
	db.First(&updatedUser, testUser.ID)
	assert.Equal(t, "Updated", *updatedUser.FirstName)
	assert.Equal(t, "Name", *updatedUser.LastName)
	assert.Equal(t, "updated@example.com", updatedUser.Email)
}

func TestUserHandler_GetUserProgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupUserHandler()

	testUser := models.User{
		ID:       uuid.New(),
		Username: "testprogress",
		Email:    "progress@example.com",
		Role:     "student",
		IsActive: true,
	}
	db.Create(&testUser)

	req, _ := http.NewRequest("GET", "/progress", nil)
	
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_id", testUser.ID)

	handler.GetUserProgress(c)

	// Should return 200 even with empty progress
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUserHandler_GetAllUsers_Admin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupUserHandler()

	// Create test users
	users := []models.User{
		{
			ID:       uuid.New(),
			Username: "user1",
			Email:    "user1@example.com",
			Role:     "student",
			IsActive: true,
		},
		{
			ID:       uuid.New(),
			Username: "user2", 
			Email:    "user2@example.com",
			Role:     "instructor",
			IsActive: true,
		},
	}

	for _, user := range users {
		db.Create(&user)
	}

	req, _ := http.NewRequest("GET", "/admin/users", nil)
	
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.GetAllUsers(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "users")
}