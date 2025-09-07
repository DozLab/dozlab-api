package interfaces

import (
	"context"
	"dozlab-backend/internal/models"
)

// UserRepository defines the interface for user data operations
type UserRepository interface {
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Create(ctx context.Context, user *models.User) error
	Update(ctx context.Context, id string, updates map[string]interface{}) error
	List(ctx context.Context, page, limit int) ([]models.User, int64, error)
	Delete(ctx context.Context, id string) error
}

// LabRepository defines the interface for lab data operations
type LabRepository interface {
	GetByID(ctx context.Context, id string) (*models.Lab, error)
	List(ctx context.Context, page, limit int, category string) ([]models.Lab, int64, error)
	Create(ctx context.Context, lab *models.Lab) error
	Update(ctx context.Context, id string, updates map[string]interface{}) error
	Delete(ctx context.Context, id string) error
}

// SessionRepository defines the interface for session data operations
type SessionRepository interface {
	GetByID(ctx context.Context, id string) (*models.Session, error)
	GetByUserID(ctx context.Context, userID string, page, limit int) ([]models.Session, int64, error)
	Create(ctx context.Context, session *models.Session) error
	UpdateStatus(ctx context.Context, id, status string) error
	Delete(ctx context.Context, id string) error
}

// ProgressRepository defines the interface for user progress data operations
type ProgressRepository interface {
	GetByUserID(ctx context.Context, userID string) ([]models.UserProgress, error)
	GetByUserAndLab(ctx context.Context, userID, labID string) (*models.UserProgress, error)
	Create(ctx context.Context, progress *models.UserProgress) error
	Update(ctx context.Context, userID, labID string, updates map[string]interface{}) error
}

// DatabaseRepository aggregates all repository interfaces
type DatabaseRepository interface {
	Users() UserRepository
	Labs() LabRepository
	Sessions() SessionRepository
	Progress() ProgressRepository
	Begin(ctx context.Context) (Transaction, error)
}