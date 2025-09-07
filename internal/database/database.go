package database

import (
	"database/sql"
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"dozlab-backend/internal/models"

	_ "github.com/lib/pq"
)

type Database struct {
	DB     *gorm.DB
	SqlDB  *sql.DB
	Config *Config
}

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

func Initialize(databaseURL string) (*Database, error) {
	// Parse database URL or use individual config
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &Database{
		DB:    db,
		SqlDB: sqlDB,
	}

	log.Println("Database connection established successfully")
	return database, nil
}

func (d *Database) Close() error {
	return d.SqlDB.Close()
}

func (d *Database) AutoMigrate() error {
	log.Println("Running auto-migration...")
	
	err := d.DB.AutoMigrate(
		&models.User{},
		&models.Lab{},
		&models.LabSpec{},
		&models.Session{},
		&models.UserProgress{},
	)
	
	if err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}
	
	log.Println("Auto-migration completed successfully")
	return nil
}

// Health check for the database
func (d *Database) HealthCheck() error {
	return d.SqlDB.Ping()
}