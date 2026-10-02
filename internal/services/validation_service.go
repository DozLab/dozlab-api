package services

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"dozlab-backend/internal/config"
)

type validationService struct {
	config *config.Config
}

// NewValidationService creates a new validation service
func NewValidationService(cfg *config.Config) ValidationService {
	return &validationService{
		config: cfg,
	}
}

// ValidateConfig validates the entire configuration
func (s *validationService) ValidateConfig(c interface{}) error {
	cfg, ok := c.(*config.Config)
	if !ok {
		return fmt.Errorf("invalid config type")
	}

	if err := s.validateRequiredFields(cfg); err != nil {
		return fmt.Errorf("required fields validation failed: %w", err)
	}

	if err := s.ValidateConnectionStrings(); err != nil {
		return fmt.Errorf("connection strings validation failed: %w", err)
	}

	if err := s.validateNumericFields(cfg); err != nil {
		return fmt.Errorf("numeric fields validation failed: %w", err)
	}

	if err := s.validateServiceURLs(cfg); err != nil {
		return fmt.Errorf("service URLs validation failed: %w", err)
	}

	return nil
}

// ValidateEnvironmentVariables validates critical environment variables
func (s *validationService) ValidateEnvironmentVariables() error {
	requiredEnvVars := []string{
		"JWT_SECRET",
		"DB_HOST",
		"DB_NAME",
		"DB_USER", 
		"DB_PASSWORD",
	}

	var missing []string
	for _, envVar := range requiredEnvVars {
		if value := getConfigValue(s.config, envVar); value == "" {
			missing = append(missing, envVar)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

// ValidateConnectionStrings validates the database connection string
func (s *validationService) ValidateConnectionStrings() error {
	// Validate database URL
	if s.config.Database.URL == "" {
		return fmt.Errorf("database URL is empty")
	}
	
	if _, err := url.Parse(s.config.Database.URL); err != nil {
		return fmt.Errorf("invalid database URL: %w", err)
	}

	return nil
}

// ValidateDependencies validates external service dependencies
func (s *validationService) ValidateDependencies(ctx context.Context) error {
	// This would typically ping external services to verify connectivity
	// For now, just validate URLs are parseable
	
	services := map[string]string{
		"WebSocket": s.config.WebSocketServiceURL,
		"Examiner":  s.config.ExaminerServiceURL,
		"Workflow":  s.config.WorkflowServiceURL,
	}

	for name, serviceURL := range services {
		if serviceURL == "" {
			return fmt.Errorf("%s service URL is empty", name)
		}
		
		if _, err := url.Parse(serviceURL); err != nil {
			return fmt.Errorf("invalid %s service URL: %w", name, err)
		}
	}

	return nil
}

// validateRequiredFields validates required configuration fields
func (s *validationService) validateRequiredFields(cfg *config.Config) error {
	if cfg.JWTSecret == "" {
		return fmt.Errorf("JWT secret is required")
	}

	if len(cfg.JWTSecret) < 32 {
		return fmt.Errorf("JWT secret must be at least 32 characters long")
	}

	if cfg.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}

	if cfg.Database.Name == "" {
		return fmt.Errorf("database name is required")
	}

	return nil
}

// validateNumericFields validates numeric configuration fields
func (s *validationService) validateNumericFields(cfg *config.Config) error {
	// Validate port numbers
	if port, err := strconv.Atoi(cfg.Database.Port); err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid database port: %s", cfg.Database.Port)
	}

	if port, err := strconv.Atoi(cfg.ServerPort); err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid server port: %s", cfg.ServerPort)
	}

	// Validate timeout values
	if cfg.ValidationTimeout <= 0 {
		return fmt.Errorf("validation timeout must be positive")
	}

	if cfg.TaskTimeout <= 0 {
		return fmt.Errorf("task timeout must be positive")
	}

	if cfg.WorkerConcurrency <= 0 {
		return fmt.Errorf("worker concurrency must be positive")
	}

	if cfg.MaxParallelTasks <= 0 {
		return fmt.Errorf("max parallel tasks must be positive")
	}

	return nil
}

// validateServiceURLs validates microservice URLs
func (s *validationService) validateServiceURLs(cfg *config.Config) error {
	urls := map[string]string{
		"WebSocket": cfg.WebSocketServiceURL,
		"Examiner":  cfg.ExaminerServiceURL,
		"Workflow":  cfg.WorkflowServiceURL,
	}

	for name, serviceURL := range urls {
		if serviceURL != "" {
			parsedURL, err := url.Parse(serviceURL)
			if err != nil {
				return fmt.Errorf("invalid %s service URL: %w", name, err)
			}
			
			if parsedURL.Scheme == "" {
				return fmt.Errorf("%s service URL missing scheme", name)
			}
			
			if parsedURL.Host == "" {
				return fmt.Errorf("%s service URL missing host", name)
			}
		}
	}

	return nil
}

// getConfigValue gets a configuration value by environment variable name
func getConfigValue(cfg *config.Config, envVar string) string {
	switch envVar {
	case "JWT_SECRET":
		return cfg.JWTSecret
	case "DB_HOST":
		return cfg.Database.Host
	case "DB_NAME":
		return cfg.Database.Name
	case "DB_USER":
		return cfg.Database.User
	case "DB_PASSWORD":
		return cfg.Database.Password
	default:
		return ""
	}
}