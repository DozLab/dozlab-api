package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	URL      string
	Host     string
	Port     string
	Name     string
	User     string
	Password string
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	URL  string
	Host string
	Port string
}

// Config holds the API service configuration
type Config struct {
	ServerPort string
	JWTSecret  string

	Database DatabaseConfig
	Redis    RedisConfig

	// Redis event bus settings
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Microservice URLs
	WebSocketServiceURL string
	ExaminerServiceURL  string
	WorkflowServiceURL  string

	// Task execution settings
	ValidationTimeout time.Duration
	TaskTimeout       time.Duration
	WorkerConcurrency int
	MaxParallelTasks  int
}

// Load reads the configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort: getEnv("PORT", "8080"),
		JWTSecret:  os.Getenv("JWT_SECRET"),
		Database: DatabaseConfig{
			URL:      os.Getenv("DATABASE_URL"),
			Host:     os.Getenv("DB_HOST"),
			Port:     getEnv("DB_PORT", "5432"),
			Name:     os.Getenv("DB_NAME"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
		},
		Redis: RedisConfig{
			URL:  os.Getenv("REDIS_URL"),
			Host: os.Getenv("REDIS_HOST"),
			Port: getEnv("REDIS_PORT", "6379"),
		},
		RedisAddr:           os.Getenv("REDIS_ADDR"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		WebSocketServiceURL: os.Getenv("WEBSOCKET_SERVICE_URL"),
		ExaminerServiceURL:  os.Getenv("EXAMINER_SERVICE_URL"),
		WorkflowServiceURL:  os.Getenv("WORKFLOW_SERVICE_URL"),
	}

	var err error
	if cfg.RedisDB, err = getEnvInt("REDIS_DB", 0); err != nil {
		return nil, err
	}
	if cfg.WorkerConcurrency, err = getEnvInt("WORKER_CONCURRENCY", 4); err != nil {
		return nil, err
	}
	if cfg.MaxParallelTasks, err = getEnvInt("MAX_PARALLEL_TASKS", 10); err != nil {
		return nil, err
	}
	if cfg.ValidationTimeout, err = getEnvDuration("VALIDATION_TIMEOUT", 30*time.Second); err != nil {
		return nil, err
	}
	if cfg.TaskTimeout, err = getEnvDuration("TASK_TIMEOUT", 5*time.Minute); err != nil {
		return nil, err
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}
