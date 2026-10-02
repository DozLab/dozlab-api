package services

import (
	"context"
	"dozlab-backend/internal/models"
)

// LabService defines the interface for lab operations
type LabService interface {
	// GetLabs returns one page of labs (page is 1-based), filtered by category when it is
	// non-empty, and the total number of matching labs.
	GetLabs(ctx context.Context, page, limit int, category string) ([]models.Lab, int64, error)
	// GetLabByID returns the lab with the given ID.
	GetLabByID(ctx context.Context, id string) (*models.Lab, error)
	// CreateLab stores a new lab.
	CreateLab(ctx context.Context, lab *models.Lab) error
	// UpdateLab applies updates (column name to value) to the lab with the given ID.
	UpdateLab(ctx context.Context, id string, updates map[string]interface{}) error
	// DeleteLab deletes the lab with the given ID; it returns an error if there is none.
	DeleteLab(ctx context.Context, id string) error
	// GetLabSpecs returns all specs of the lab labID.
	GetLabSpecs(ctx context.Context, labID string) ([]models.LabSpec, error)
	// CreateLabSpec stores a new lab spec.
	CreateLabSpec(ctx context.Context, spec *models.LabSpec) error
	// GetLabSpecByID returns the lab spec with the given ID.
	GetLabSpecByID(ctx context.Context, id string) (*models.LabSpec, error)
	// UpdateLabSpec applies updates (column name to value) to the lab spec with the given ID.
	UpdateLabSpec(ctx context.Context, id string, updates map[string]interface{}) error
	// DeleteLabSpec deletes the lab spec with the given ID; it returns an error if there is none.
	DeleteLabSpec(ctx context.Context, id string) error
}

// UserService defines the interface for user operations
type UserService interface {
	// GetUserByID returns the user with the given ID.
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	// GetUserByUsername returns the user with the given username.
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	// GetUserByEmail returns the user with the given email address.
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	// CreateUser stores a new user.
	CreateUser(ctx context.Context, user *models.User) error
	// UpdateUser applies updates (column name to value) to the user with the given ID.
	UpdateUser(ctx context.Context, id string, updates map[string]interface{}) error
	// GetAllUsers returns one page of users (page is 1-based) and the total number of users.
	GetAllUsers(ctx context.Context, page, limit int) ([]models.User, int64, error)
	// GetUserProgress returns the user's progress records for all labs.
	GetUserProgress(ctx context.Context, userID string) ([]models.UserProgress, error)
	// GetLabProgress returns the user's progress on one lab; it returns an error if there is none.
	GetLabProgress(ctx context.Context, userID, labID string) (*models.UserProgress, error)
}

// AuthService defines the interface for authentication operations
type AuthService interface {
	// Register checks the password strength, rejects a taken username or email, and creates
	// the user.
	Register(ctx context.Context, req *models.UserRegistrationRequest) (*models.User, error)
	// Login checks the username and password of an active user and returns the user and an
	// access token.
	Login(ctx context.Context, username, password string) (*models.User, string, error)
	// RefreshToken exchanges a valid refresh token for a new access token.
	RefreshToken(ctx context.Context, token string) (string, error)
	// ValidateToken validates an access token and returns its user, who must be active.
	ValidateToken(ctx context.Context, token string) (*models.User, error)
}

// SessionService defines the interface for session operations
type SessionService interface {
	// GetSessions returns one page of the user's sessions (page is 1-based) and the user's
	// total number of sessions.
	GetSessions(ctx context.Context, userID string, page, limit int) ([]models.Session, int64, error)
	// CreateSession stores a new session.
	CreateSession(ctx context.Context, session *models.Session) error
	// GetSessionByID returns the session with the given ID.
	GetSessionByID(ctx context.Context, id string) (*models.Session, error)
	// UpdateSessionStatus sets the status of the session with the given ID; it returns an error
	// if there is none.
	UpdateSessionStatus(ctx context.Context, id, status string) error
	// DeleteSession deletes the session with the given ID; it returns an error if there is none.
	DeleteSession(ctx context.Context, id string) error
}

// EventBusService defines the interface for event publishing with proper error handling
type EventBusService interface {
	// PublishEvent publishes event on the event bus.
	PublishEvent(ctx context.Context, event interface{}) error
	PublishCriticalEvent(ctx context.Context, event interface{}) error // Returns error that should halt operations
	// RegisterHandler registers handler for events of eventType. Handlers run for the event
	// types passed to Subscribe.
	RegisterHandler(eventType string, handler func(ctx context.Context, event interface{}) error)
	// Subscribe starts delivering events of eventTypes to their registered handlers.
	Subscribe(ctx context.Context, eventTypes []string) error
}

// ValidationService defines the interface for configuration validation
type ValidationService interface {
	// ValidateConfig validates a *config.Config: required fields, connection strings and
	// numeric settings.
	ValidateConfig(config interface{}) error
	// ValidateEnvironmentVariables reports which required settings (JWT_SECRET, DB_HOST, DB_NAME,
	// DB_USER, DB_PASSWORD) are empty in the loaded config.
	ValidateEnvironmentVariables() error
	// ValidateConnectionStrings checks that the database URL is set and parses.
	ValidateConnectionStrings() error
	// ValidateDependencies checks that the WebSocket, Examiner and Workflow service URLs are set
	// and parse.
	ValidateDependencies(ctx context.Context) error
}