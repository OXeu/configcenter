package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

func (s *Store) ensurePrimaryEnvironments(ctx context.Context) error {
	var projects []Project
	if err := s.db.WithContext(ctx).Where("primary_environment_id = ?", 0).Find(&projects).Error; err != nil {
		return fmt.Errorf("list projects without primary environment: %w", err)
	}
	for _, project := range projects {
		if err := s.initializePrimaryEnvironment(ctx, project.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) initializePrimaryEnvironment(ctx context.Context, projectID uint) error {
	return s.Transaction(ctx, func(tx *Store) error {
		project, err := tx.projectForUpdate(ctx, projectID)
		if err != nil {
			return err
		}
		if project.PrimaryEnvironmentID != 0 {
			return nil
		}
		var env Environment
		err = tx.db.Where("project_id = ?", projectID).Order("CASE WHEN slug = 'production' THEN 0 ELSE 1 END").Order("id").First(&env).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find initial primary environment: %w", err)
		}
		return tx.setPrimaryEnvironment(ctx, projectID, env.ID)
	})
}

func (s *Store) setPrimaryEnvironment(ctx context.Context, projectID, envID uint) error {
	return s.mustAffect(ctx, s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Update("primary_environment_id", envID), "set primary environment")
}

// SetPrimaryEnvironment selects the environment opened by default for a project.
func (s *Store) SetPrimaryEnvironment(ctx context.Context, projectID, envID uint) error {
	return s.Transaction(ctx, func(tx *Store) error {
		project, _, err := tx.environmentForUpdate(ctx, projectID, envID)
		if err != nil {
			return err
		}
		if project.PrimaryEnvironmentID == envID {
			return nil
		}
		return tx.setPrimaryEnvironment(ctx, projectID, envID)
	})
}
