package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// ErrRegistrationClosed indicates that automatic account creation is disabled.
var ErrRegistrationClosed = errors.New("registration closed")

func (s *Store) settingsEnvironment(ctx context.Context) (Project, Environment, error) {
	p, err := s.ProjectBySlug(ctx, "configcenter")
	if err != nil {
		return Project{}, Environment{}, err
	}
	e, err := s.EnvironmentBySlug(ctx, p.ID, "production")
	return p, e, err
}

// RegistrationSetting reads the registration configuration and its secret flag.
func (s *Store) RegistrationSetting(ctx context.Context) (ConfigItem, error) {
	p, e, err := s.settingsEnvironment(ctx)
	if err != nil {
		return ConfigItem{}, err
	}
	item, err := s.GetItem(ctx, p.ID, e.ID, "allow_registration")
	if errors.Is(err, ErrNotFound) {
		return ConfigItem{Key: "allow_registration", Value: "true"}, nil
	}
	return item, err
}

// RegistrationAllowed reads the registration policy from the internal project.
func (s *Store) RegistrationAllowed(ctx context.Context) (bool, error) {
	item, err := s.RegistrationSetting(ctx)
	if err != nil {
		return false, err
	}
	allowed, err := strconv.ParseBool(item.Value)
	if err != nil {
		return false, fmt.Errorf("parse registration policy: %w", strconv.ErrSyntax)
	}
	return allowed, nil
}

// SetRegistrationAllowed persists the registration policy in the internal project.
func (s *Store) SetRegistrationAllowed(ctx context.Context, allowed bool, userID uint) error {
	p, e, err := s.settingsEnvironment(ctx)
	if err != nil {
		return err
	}
	_, err = s.PutItem(ctx, ItemInput{ProjectID: p.ID, EnvironmentID: e.ID, Key: "allow_registration", Value: strconv.FormatBool(allowed), UpdatedBy: userID})
	return err
}
