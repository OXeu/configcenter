package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm/clause"
)

// ErrSystemEnvironment protects the environment used by system settings.
var ErrSystemEnvironment = errors.New("system environment cannot be renamed or deleted")

// ErrPrimaryEnvironment requires selecting a replacement before deletion.
var ErrPrimaryEnvironment = errors.New("select another primary environment before deleting this one")

// ListEnvironments returns the environments of a project.
func (s *Store) ListEnvironments(ctx context.Context, projectID uint) ([]Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Order("slug").Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	return envs, nil
}

// EnvironmentBySlug loads an environment inside a project.
func (s *Store) EnvironmentBySlug(ctx context.Context, projectID uint, slug string) (Environment, error) {
	var e Environment
	err := s.db.WithContext(ctx).Where("project_id = ? AND slug = ?", projectID, slug).First(&e).Error
	return e, queryErr("load environment", err)
}

// CreateEnvironment adds an environment to a project.
func (s *Store) CreateEnvironment(ctx context.Context, e Environment) (Environment, error) {
	err := s.Transaction(ctx, func(tx *Store) error {
		project, err := tx.projectForUpdate(ctx, e.ProjectID)
		if err != nil {
			return err
		}
		if err := tx.db.Create(&e).Error; err != nil {
			return fmt.Errorf("create environment: %w", err)
		}
		if project.PrimaryEnvironmentID == 0 {
			return tx.setPrimaryEnvironment(ctx, project.ID, e.ID)
		}
		return nil
	})
	if err != nil {
		return Environment{}, err
	}
	return e, nil
}

func (s *Store) projectForUpdate(ctx context.Context, projectID uint) (Project, error) {
	var project Project
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, projectID).Error
	return project, queryErr("lock environment project", err)
}

func (s *Store) environmentForUpdate(ctx context.Context, projectID, id uint) (Project, Environment, error) {
	project, err := s.projectForUpdate(ctx, projectID)
	if err != nil {
		return Project{}, Environment{}, err
	}
	var env Environment
	err = s.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&env).Error
	return project, env, queryErr("load environment for update", err)
}

// RenameEnvironment changes an environment's connection slug and display name.
func (s *Store) RenameEnvironment(ctx context.Context, projectID, id uint, slug, name string) error {
	return s.Transaction(ctx, func(tx *Store) error {
		project, env, err := tx.environmentForUpdate(ctx, projectID, id)
		if err != nil {
			return err
		}
		if project.Slug == "configcenter" && env.Slug == "production" && slug != env.Slug {
			return ErrSystemEnvironment
		}
		if env.Slug == slug && env.Name == name {
			return nil
		}
		return tx.mustAffect(ctx, tx.db.Model(&Environment{}).Where("project_id = ? AND id = ?", projectID, id).Updates(map[string]any{"slug": slug, "name": name}), "rename environment")
	})
}

// DeleteEnvironment removes an environment, its configuration and its proposals.
func (s *Store) DeleteEnvironment(ctx context.Context, projectID, id uint) error {
	return s.Transaction(ctx, func(tx *Store) error {
		project, env, err := tx.environmentForUpdate(ctx, projectID, id)
		if err != nil {
			return err
		}
		if project.Slug == "configcenter" && env.Slug == "production" {
			return ErrSystemEnvironment
		}
		if project.PrimaryEnvironmentID == id {
			return ErrPrimaryEnvironment
		}
		for _, model := range []any{&ConfigItem{}, &MCPProposal{}} {
			if err := tx.db.Where("project_id = ? AND environment_id = ?", projectID, id).Delete(model).Error; err != nil {
				return fmt.Errorf("delete environment data: %w", err)
			}
		}
		return tx.mustAffect(ctx, tx.db.Where("project_id = ? AND id = ?", projectID, id).Delete(&Environment{}), "delete environment")
	})
}
