package services

import (
	"context"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"dozlab-backend/pkg/auth"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type authService struct {
	db        *database.Database
	eventBus  EventBusService
	jwtSecret string
}

func NewAuthService(db *database.Database, eventBus EventBusService, jwtSecret string) AuthService {
	return &authService{
		db:        db,
		eventBus:  eventBus,
		jwtSecret: jwtSecret,
	}
}

func (s *authService) Register(ctx context.Context, req *models.UserRegistrationRequest) (*models.User, error) {
	if err := auth.ValidatePasswordStrength(req.Password); err != nil {
		return nil, fmt.Errorf("password validation failed: %w", err)
	}

	var existingUser models.User
	if err := s.db.DB.WithContext(ctx).Where("username = ? OR email = ?", req.Username, req.Email).First(&existingUser).Error; err == nil {
		return nil, fmt.Errorf("user with username %s or email %s already exists", req.Username, req.Email)
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hashedPassword,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Role:         "student",
		IsActive:     true,
	}

	if err := s.db.DB.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"type":     "user.registered",
			"user_id":  user.ID.String(),
			"username": user.Username,
			"email":    user.Email,
			"role":     user.Role,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return &user, nil
}

func (s *authService) Login(ctx context.Context, username, password string) (*models.User, string, error) {
	var user models.User
	if err := s.db.DB.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", fmt.Errorf("invalid username or password")
		}
		return nil, "", fmt.Errorf("database error: %w", err)
	}

	if !user.IsActive {
		return nil, "", fmt.Errorf("user account is inactive")
	}

	if !auth.CheckPassword(password, user.PasswordHash) {
		return nil, "", fmt.Errorf("invalid username or password")
	}

	tokens, err := auth.GenerateTokenPair(user.ID, user.Username, user.Email, user.Role, s.jwtSecret)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate tokens: %w", err)
	}

	if err := s.db.DB.WithContext(ctx).Model(&user).Update("last_login_at", time.Now()).Error; err != nil {
	}

	if s.eventBus != nil {
		event := map[string]interface{}{
			"type":    "user.logged_in",
			"user_id": user.ID.String(),
			"email":   user.Email,
		}
		if err := s.eventBus.PublishEvent(ctx, event); err != nil {
		}
	}

	return &user, tokens.AccessToken, nil
}

func (s *authService) RefreshToken(ctx context.Context, token string) (string, error) {
	tokens, err := auth.RefreshAccessToken(token, s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("invalid or expired refresh token: %w", err)
	}

	return tokens.AccessToken, nil
}

func (s *authService) ValidateToken(ctx context.Context, token string) (*models.User, error) {
	claims, err := auth.ValidateToken(token, s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	var user models.User
	if err := s.db.DB.WithContext(ctx).First(&user, "id = ?", claims.UserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	if !user.IsActive {
		return nil, fmt.Errorf("user account is inactive")
	}

	return &user, nil
}