package interfaces

import (
	"context"
	"dozlab-backend/internal/models"
)

// Transaction interface for database transactions
type Transaction interface {
	Commit() error
	Rollback() error
	Users() UserRepository
	Labs() LabRepository
	Sessions() SessionRepository
	Progress() ProgressRepository
}

// AuthService defines authentication operations interface for dependency injection
type AuthServiceInterface interface {
	Register(ctx context.Context, req *models.UserRegistrationRequest) (*models.User, string, error)
	Login(ctx context.Context, username, password string) (*models.User, string, error)
	RefreshToken(ctx context.Context, token string) (string, error)
	ValidateToken(ctx context.Context, token string) (*models.User, error)
	Logout(ctx context.Context, token string) error
}

// UserService defines user operations interface for dependency injection  
type UserServiceInterface interface {
	GetProfile(ctx context.Context, userID string) (*models.User, error)
	UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) error
	GetAllUsers(ctx context.Context, page, limit int) ([]models.User, int64, error)
	UpdateUserRole(ctx context.Context, userID, role string) error
	UpdateUserStatus(ctx context.Context, userID string, isActive bool) error
	GetUserProgress(ctx context.Context, userID string) ([]models.UserProgress, error)
	GetLabProgress(ctx context.Context, userID, labID string) (*models.UserProgress, error)
}

// LabService defines lab operations interface for dependency injection
type LabServiceInterface interface {
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

// SessionService defines session operations interface for dependency injection
type SessionServiceInterface interface {
	GetSessions(ctx context.Context, userID string, page, limit int) ([]models.Session, int64, error)
	CreateSession(ctx context.Context, session *models.Session) error
	GetSessionByID(ctx context.Context, id string) (*models.Session, error)
	UpdateSessionStatus(ctx context.Context, id, status string) error
	DeleteSession(ctx context.Context, id string) error
}

// EventBusService defines event bus operations interface for dependency injection
type EventBusServiceInterface interface {
	PublishEvent(ctx context.Context, event interface{}) error
	PublishCriticalEvent(ctx context.Context, event interface{}) error
	RegisterHandler(eventType string, handler func(ctx context.Context, event interface{}) error)
	Subscribe(ctx context.Context, eventTypes []string) error
}

// ValidationService defines validation operations interface for dependency injection  
type ValidationServiceInterface interface {
	ValidateConfig(config interface{}) error
	ValidateEnvironmentVariables() error
	ValidateConnectionStrings() error
	ValidateDependencies(ctx context.Context) error
}