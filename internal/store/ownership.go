package store

import (
	"context"
	"fmt"
)

func (s *Store) ensureSystemProject(ctx context.Context) error {
	return s.Transaction(ctx, func(tx *Store) error {
		var admin User
		if err := tx.db.Where("role = ?", RoleAdmin).Order("id").First(&admin).Error; err != nil {
			return fmt.Errorf("find administrator: %w", err)
		}
		var count int64
		if err := tx.db.Model(&Project{}).Where("slug = ?", "configcenter").Count(&count).Error; err != nil {
			return fmt.Errorf("find internal project: %w", err)
		}
		if count == 0 {
			if _, err := tx.CreateProject(ctx, Project{Slug: "configcenter", Name: "Configcenter", OwnerID: admin.ID}, Environment{Slug: "production", Name: "Production"}); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteUser transfers projects and removes credentials before deleting the account.
func (s *Store) DeleteUser(ctx context.Context, id, successor uint) error {
	return s.Transaction(ctx, func(tx *Store) error {
		if err := tx.db.Model(&Project{}).Where("owner_id = ?", id).Update("owner_id", successor).Error; err != nil {
			return fmt.Errorf("transfer projects: %w", err)
		}
		if err := tx.db.Where("user_id = ?", id).Delete(&Session{}).Error; err != nil {
			return fmt.Errorf("delete user sessions: %w", err)
		}
		if err := tx.db.Where("created_by = ?", id).Delete(&APIKey{}).Error; err != nil {
			return fmt.Errorf("delete user keys: %w", err)
		}
		if err := tx.revokeMCPUserGrants(ctx, id); err != nil {
			return err
		}
		return tx.mustAffect(ctx, tx.db.Delete(&User{}, id), "delete user")
	})
}

// RotateAPIKey replaces a scoped key's credential.
func (s *Store) RotateAPIKey(ctx context.Context, id uint, revision uint64, digest []byte, prefix string) error {
	return keyMutation(s.db.WithContext(ctx).Model(&APIKey{}).Where("id = ? AND revision = ?", id, revision).Updates(map[string]any{"digest": digest, "prefix": prefix, "last_used_at": nil, "revision": revision + 1}), "rotate key")
}
