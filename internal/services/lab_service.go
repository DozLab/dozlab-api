package services

import (
	"context"
	"fmt"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"gorm.io/gorm"
)

type labService struct {
	db *database.Database
}

// NewLabService creates a new lab service instance
func NewLabService(db *database.Database) LabService {
	return &labService{db: db}
}

func (s *labService) GetLabs(ctx context.Context, page, limit int, category string) ([]models.Lab, int64, error) {
	var labs []models.Lab
	var total int64

	query := s.db.DB.WithContext(ctx).Model(&models.Lab{})
	
	if category != "" {
		query = query.Where("category = ?", category)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count labs: %w", err)
	}

	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Find(&labs).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get labs: %w", err)
	}

	return labs, total, nil
}

func (s *labService) GetLabByID(ctx context.Context, id string) (*models.Lab, error) {
	var lab models.Lab
	if err := s.db.DB.WithContext(ctx).Where("id = ?", id).First(&lab).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("lab not found")
		}
		return nil, fmt.Errorf("failed to get lab: %w", err)
	}
	return &lab, nil
}

func (s *labService) CreateLab(ctx context.Context, lab *models.Lab) error {
	if err := s.db.DB.WithContext(ctx).Create(lab).Error; err != nil {
		return fmt.Errorf("failed to create lab: %w", err)
	}
	return nil
}

func (s *labService) UpdateLab(ctx context.Context, id string, updates map[string]interface{}) error {
	result := s.db.DB.WithContext(ctx).Model(&models.Lab{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update lab: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("lab not found")
	}
	return nil
}

func (s *labService) DeleteLab(ctx context.Context, id string) error {
	result := s.db.DB.WithContext(ctx).Delete(&models.Lab{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete lab: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("lab not found")
	}
	return nil
}

func (s *labService) GetLabSpecs(ctx context.Context, labID string) ([]models.LabSpec, error) {
	var specs []models.LabSpec
	if err := s.db.DB.WithContext(ctx).Where("lab_id = ?", labID).Find(&specs).Error; err != nil {
		return nil, fmt.Errorf("failed to get lab specs: %w", err)
	}
	return specs, nil
}

func (s *labService) CreateLabSpec(ctx context.Context, spec *models.LabSpec) error {
	if err := s.db.DB.WithContext(ctx).Create(spec).Error; err != nil {
		return fmt.Errorf("failed to create lab spec: %w", err)
	}
	return nil
}

func (s *labService) GetLabSpecByID(ctx context.Context, id string) (*models.LabSpec, error) {
	var spec models.LabSpec
	if err := s.db.DB.WithContext(ctx).Where("id = ?", id).First(&spec).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("lab spec not found")
		}
		return nil, fmt.Errorf("failed to get lab spec: %w", err)
	}
	return &spec, nil
}

func (s *labService) UpdateLabSpec(ctx context.Context, id string, updates map[string]interface{}) error {
	result := s.db.DB.WithContext(ctx).Model(&models.LabSpec{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update lab spec: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("lab spec not found")
	}
	return nil
}

func (s *labService) DeleteLabSpec(ctx context.Context, id string) error {
	result := s.db.DB.WithContext(ctx).Delete(&models.LabSpec{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete lab spec: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("lab spec not found")
	}
	return nil
}