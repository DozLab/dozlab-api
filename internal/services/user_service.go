package services

import (
	"context"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type userService struct {
	db       *database.Database
	eventBus EventBusService
}

func NewUserService(db *database.Database, eventBus EventBusService) UserService {
	return &userService{
		db:       db,
		eventBus: eventBus,
	}
}

func (s *userService) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	userID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	var user models.User
	if err := s.db.DB.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (s *userService) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	if err := s.db.DB.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found: %s", username)
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (s *userService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	if err := s.db.DB.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found: %s", email)
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (s *userService) CreateUser(ctx context.Context, user *models.User) error {
	if err := s.db.DB.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"user_id":  user.ID.String(),
			"username": user.Username,
			"email":    user.Email,
			"role":     user.Role,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return nil
}

func (s *userService) UpdateUser(ctx context.Context, id string, updates map[string]interface{}) error {
	userID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	if err := s.db.DB.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

func (s *userService) GetAllUsers(ctx context.Context, page, limit int) ([]models.User, int64, error) {
	var users []models.User
	var total int64

	if err := s.db.DB.WithContext(ctx).Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	offset := (page - 1) * limit
	if err := s.db.DB.WithContext(ctx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get users: %w", err)
	}

	return users, total, nil
}

func (s *userService) GetUserProgress(ctx context.Context, userID string) ([]models.UserProgress, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	var progress []models.UserProgress
	if err := s.db.DB.WithContext(ctx).Preload("Lab").Where("user_id = ?", userUUID).Find(&progress).Error; err != nil {
		return nil, fmt.Errorf("failed to get user progress: %w", err)
	}

	return progress, nil
}

func (s *userService) GetLabProgress(ctx context.Context, userID, labID string) (*models.UserProgress, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	labUUID, err := uuid.Parse(labID)
	if err != nil {
		return nil, fmt.Errorf("invalid lab ID: %w", err)
	}

	var progress models.UserProgress
	if err := s.db.DB.WithContext(ctx).Preload("Lab").Where("user_id = ? AND lab_id = ?", userUUID, labUUID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("progress not found for user %s and lab %s", userID, labID)
		}
		return nil, fmt.Errorf("failed to get lab progress: %w", err)
	}

	return &progress, nil
}