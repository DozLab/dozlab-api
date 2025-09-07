package services

import (
	"context"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type sessionService struct {
	db       *database.Database
	eventBus EventBusService
}

func NewSessionService(db *database.Database, eventBus EventBusService) SessionService {
	return &sessionService{
		db:       db,
		eventBus: eventBus,
	}
}

func (s *sessionService) GetSessions(ctx context.Context, userID string, page, limit int) ([]models.Session, int64, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid user ID: %w", err)
	}

	var sessions []models.Session
	var total int64

	query := s.db.DB.WithContext(ctx).Model(&models.Session{}).Where("user_id = ?", userUUID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count sessions: %w", err)
	}

	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Find(&sessions).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get sessions: %w", err)
	}

	return sessions, total, nil
}

func (s *sessionService) CreateSession(ctx context.Context, session *models.Session) error {
	if err := s.db.DB.WithContext(ctx).Create(session).Error; err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"type":       "session.created",
			"session_id": session.ID.String(),
			"user_id":    session.UserID.String(),
			"lab_id":     session.LabID.String(),
			"status":     session.Status,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return nil
}

func (s *sessionService) GetSessionByID(ctx context.Context, id string) (*models.Session, error) {
	sessionID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	var session models.Session
	if err := s.db.DB.WithContext(ctx).Preload("Lab").Preload("User").First(&session, "id = ?", sessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("session not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

func (s *sessionService) UpdateSessionStatus(ctx context.Context, id, status string) error {
	sessionID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	result := s.db.DB.WithContext(ctx).Model(&models.Session{}).Where("id = ?", sessionID).Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("failed to update session status: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"type":       "session.status_updated",
			"session_id": id,
			"status":     status,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return nil
}

func (s *sessionService) DeleteSession(ctx context.Context, id string) error {
	sessionID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	result := s.db.DB.WithContext(ctx).Delete(&models.Session{}, "id = ?", sessionID)
	if result.Error != nil {
		return fmt.Errorf("failed to delete session: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"type":       "session.deleted",
			"session_id": id,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return nil
}