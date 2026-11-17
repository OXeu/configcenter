package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm/clause"
)

// ErrSystemEnvironment protects the environment used by system settings.
var ErrSystemEnvironment = errors.New("system environment cannot be renamed or deleted")

// ErrLastEnvironment prevents projects from losing their last environment.
var ErrLastEnvironment = errors.New("project must retain an environment")

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
	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		return Environment{}, fmt.Errorf("create environment: %w", err)
	}
	return e, nil
}

func (s *Store) environmentForUpdate(ctx context.Context, projectID, id uint) (Project, Environment, error) {
	var project Project
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, projectID).Error; err != nil {
		return Project{}, Environment{}, queryErr("lock environment project", err)
	}
	var env Environment
	err := s.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&env).Error
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
		var count int64
		if err := tx.db.Model(&Environment{}).Where("project_id = ?", projectID).Count(&count).Error; err != nil {
			return fmt.Errorf("count project environments: %w", err)
		}
		if count <= 1 {
			return ErrLastEnvironment
		}
		for _, model := range []any{&ConfigItem{}, &MCPProposal{}} {
			if err := tx.db.Where("project_id = ? AND environment_id = ?", projectID, id).Delete(model).Error; err != nil {
				return fmt.Errorf("delete environment data: %w", err)
			}
		}
		return tx.mustAffect(ctx, tx.db.Where("project_id = ? AND id = ?", projectID, id).Delete(&Environment{}), "delete environment")
	})
}
