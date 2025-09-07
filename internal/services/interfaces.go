package services

import (
	"context"
	"dozlab-backend/internal/models"
)

// LabService defines the interface for lab operations
type LabService interface {
	GetLabs(ctx context.Context, page, limit int, category string) ([]models.Lab, int64, error)
	GetLabByID(ctx context.Context, id string) (*models.Lab, error)
	CreateLab(ctx context.Context, lab *models.Lab) error
	UpdateLab(ctx context.Context, id string, updates map[string]interface{}) error
	DeleteLab(ctx context.Context, id string) error
	GetLabSpecs(ctx context.Context, labID string) ([]models.LabSpec, error)
	CreateLabSpec(ctx context.Context, spec *models.LabSpec) error
	GetLabSpecByID(ctx context.Context, id string) (*models.LabSpec, error)
	UpdateLabSpec(ctx context.Context, id string, updates map[string]interface{}) error
	DeleteLabSpec(ctx context.Context, id string) error
}

// UserService defines the interface for user operations
type UserService interface {
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, id string, updates map[string]interface{}) error
	GetAllUsers(ctx context.Context, page, limit int) ([]models.User, int64, error)
	GetUserProgress(ctx context.Context, userID string) ([]models.UserProgress, error)
	GetLabProgress(ctx context.Context, userID, labID string) (*models.UserProgress, error)
}

// AuthService defines the interface for authentication operations
type AuthService interface {
	Register(ctx context.Context, req *models.UserRegistrationRequest) (*models.User, error)
	Login(ctx context.Context, username, password string) (*models.User, string, error)
	RefreshToken(ctx context.Context, token string) (string, error)
	ValidateToken(ctx context.Context, token string) (*models.User, error)
}

// SessionService defines the interface for session operations
type SessionService interface {
	GetSessions(ctx context.Context, userID string, page, limit int) ([]models.Session, int64, error)
	CreateSession(ctx context.Context, session *models.Session) error
	GetSessionByID(ctx context.Context, id string) (*models.Session, error)
	UpdateSessionStatus(ctx context.Context, id, status string) error
	DeleteSession(ctx context.Context, id string) error
}

// EventBusService defines the interface for event publishing with proper error handling
type EventBusService interface {
	PublishEvent(ctx context.Context, event interface{}) error
	PublishCriticalEvent(ctx context.Context, event interface{}) error // Returns error that should halt operations
	RegisterHandler(eventType string, handler func(ctx context.Context, event interface{}) error)
	Subscribe(ctx context.Context, eventTypes []string) error
	GetEvent(ctx context.Context, eventID string) (interface{}, error)
	ListEvents(ctx context.Context, filters map[string]interface{}) ([]interface{}, error)
}

// ValidationService defines the interface for configuration validation
type ValidationService interface {
	ValidateConfig(config interface{}) error
	ValidateEnvironmentVariables() error
	ValidateConnectionStrings() error
	ValidateDependencies(ctx context.Context) error
}