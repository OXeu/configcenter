package store

import (
	"context"
	"strings"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

// ConfigurationSource reads plaintext settings directly from one database environment.
// It is for trusted in-process use; HTTP callers still require authorization.
func (s *Store) ConfigurationSource(project, environment string) configcenter.DataSource {
	return configurationSource{store: s, project: project, environment: environment}
}

type configurationSource struct {
	store       *Store
	project     string
	environment string
}

func (s configurationSource) Read(ctx context.Context, prefix string) ([]configcenter.Item, error) {
	project, err := s.store.ProjectBySlug(ctx, s.project)
	if err != nil {
		return nil, err
	}
	environment, err := s.store.EnvironmentBySlug(ctx, project.ID, s.environment)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListItems(ctx, project.ID, environment.ID, prefix)
	if err != nil {
		return nil, err
	}
	values := make([]configcenter.Item, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item.Key, prefix) {
			continue
		}
		values = append(values, configcenter.Item{
			Key: item.Key, Value: item.Value, Secret: item.Secret,
			Description: item.Description, UpdatedAt: item.UpdatedAt,
		})
	}
	return values, nil
}
