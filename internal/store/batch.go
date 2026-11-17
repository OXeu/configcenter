package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// ItemInput is a configuration write; a nil Secret preserves the existing flag.
type ItemInput struct {
	ProjectID     uint
	EnvironmentID uint
	Key           string
	Value         string
	Description   string
	UpdatedBy     uint
	Secret        *bool
}

// GetItems reads selected keys within one project and environment.
func (s *Store) GetItems(ctx context.Context, projectID, envID uint, keys []string) ([]ConfigItem, error) {
	items := []ConfigItem{}
	if err := s.db.WithContext(ctx).Where("project_id = ? AND environment_id = ?", projectID, envID).Where(clause.IN{Column: "key", Values: keyValues(keys)}).Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("read configuration keys: %w", err)
	}
	return items, nil
}

func keyValues(keys []string) []any {
	values := make([]any, len(keys))
	for i, key := range keys {
		values[i] = key
	}
	return values
}

// PutItems atomically creates or updates all supplied configuration items.
func (s *Store) PutItems(ctx context.Context, items []ItemInput) error {
	if len(items) == 0 {
		return nil
	}
	return s.Transaction(ctx, func(tx *Store) error {
		query := tx.db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
		for _, updateSecret := range []bool{false, true} {
			batch := []ConfigItem{}
			for _, input := range items {
				if (input.Secret != nil) != updateSecret {
					continue
				}
				item := ConfigItem{
					ProjectID: input.ProjectID, EnvironmentID: input.EnvironmentID,
					Key: input.Key, Value: input.Value, Description: input.Description, UpdatedBy: input.UpdatedBy,
				}
				if input.Secret != nil {
					item.Secret = *input.Secret
				}
				batch = append(batch, item)
			}
			if len(batch) == 0 {
				continue
			}
			columns := []string{"value", "description", "updated_by", "updated_at"}
			if updateSecret {
				columns = append(columns, "secret")
			}
			conflict := clause.OnConflict{
				Columns:   []clause.Column{{Name: "project_id"}, {Name: "environment_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns(columns),
			}
			if err := query.Clauses(conflict).CreateInBatches(&batch, 100).Error; err != nil {
				return fmt.Errorf("write configuration batch: %w", err)
			}
			for _, item := range batch {
				if item.Secret {
					if err := tx.protectProposals(ctx, item.ProjectID, item.EnvironmentID, item.Key); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (s *Store) protectProposals(ctx context.Context, projectID, envID uint, key string) error {
	if err := s.db.WithContext(ctx).Model(&MCPProposal{}).
		Where("project_id = ? AND environment_id = ? AND `key` = ?", projectID, envID, key).
		Update("secret", true).Error; err != nil {
		return fmt.Errorf("protect configuration proposals: %w", err)
	}
	return nil
}
