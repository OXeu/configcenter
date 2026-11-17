package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// ErrNotFound is returned when a scoped lookup finds no row.
var ErrNotFound = errors.New("not found")

// Store owns the database handle and every query.
type Store struct {
	db *gorm.DB
}

// Transaction runs fn inside a database transaction; fn receives a Store
// bound to the transaction.
func (s *Store) Transaction(ctx context.Context, fn func(tx *Store) error) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Store{db: tx})
	})
	if err != nil {
		return fmt.Errorf("transaction: %w", err)
	}
	return nil
}

// Open migrates the schema and ensures the bootstrap administrator exists.
func Open(ctx context.Context, dialector gorm.Dialector, adminEmail, adminName string) (*Store, error) {
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.WithContext(ctx).AutoMigrate(
		&User{}, &Session{}, &Project{}, &Environment{}, &ConfigItem{}, &APIKey{}, &MCPProposal{},
		&MCPClient{}, &MCPAuthorization{}, &MCPGrant{}, &MCPToken{},
	); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	s := &Store{db: db}
	if err := s.ensureAdmin(ctx, adminEmail, adminName); err != nil {
		return nil, err
	}
	if err := s.ensureSystemProject(ctx); err != nil {
		return nil, err
	}
	if err := s.ensurePrimaryEnvironments(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureAdmin(ctx context.Context, email, name string) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&User{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	admin := User{Email: email, Name: name, Role: RoleAdmin}
	if err := s.db.WithContext(ctx).Create(&admin).Error; err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}

func queryErr(op string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// UserByID loads a user.
func (s *Store) UserByID(ctx context.Context, id uint) (User, error) {
	var u User
	err := s.db.WithContext(ctx).First(&u, id).Error
	return u, queryErr("load user", err)
}

// UserByEmail loads a user by email.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	return u, queryErr("load user by email", err)
}

// UserBySubject loads a user linked to an OAuth issuer/subject pair.
func (s *Store) UserBySubject(ctx context.Context, issuer, sub string) (User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("oauth_issuer = ? AND oauth_sub = ?", issuer, sub).First(&u).Error
	return u, queryErr("load user by subject", err)
}

// LinkOAuthUser links an OAuth identity to a user, creating it on first login.
// Identity.Role is applied only when the user is new; existing roles are
// managed in the user admin screen.
func (s *Store) LinkOAuthUser(ctx context.Context, identity Identity) (User, error) {
	u, err := s.UserBySubject(ctx, identity.Issuer, identity.Sub)
	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound):
		u, err = s.adoptByEmail(ctx, identity)
		if err != nil {
			return User{}, err
		}
	default:
		return User{}, err
	}
	updates := map[string]any{
		"oauth_issuer":  identity.Issuer,
		"oauth_sub":     identity.Sub,
		"name":          identity.Name,
		"avatar_url":    identity.AvatarURL,
		"last_login_at": identity.Now,
	}
	if err := s.db.WithContext(ctx).Model(&u).Updates(updates).Error; err != nil {
		return User{}, fmt.Errorf("update oauth user: %w", err)
	}
	return s.UserByID(ctx, u.ID)
}

func (s *Store) adoptByEmail(ctx context.Context, identity Identity) (User, error) {
	u, err := s.UserByEmail(ctx, identity.Email)
	switch {
	case err == nil:
		return u, nil
	case errors.Is(err, ErrNotFound):
		allowed, policyErr := s.RegistrationAllowed(ctx)
		if policyErr != nil {
			return User{}, policyErr
		}
		if !allowed {
			return User{}, ErrRegistrationClosed
		}
		u = User{Email: identity.Email, Name: identity.Name, Role: identity.Role}
		if u.Role != RoleAdmin {
			u.Role = RoleUser
		}
		if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
			return User{}, fmt.Errorf("create oauth user: %w", err)
		}
		return u, nil
	default:
		return User{}, err
	}
}

// CreateUser inserts a user without an OAuth identity, for local accounts.
func (s *Store) CreateUser(ctx context.Context, u User) (User, error) {
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return s.UserByID(ctx, u.ID)
}

// ListUsers returns all users ordered by id.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	if err := s.db.WithContext(ctx).Order("id").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// SetUserRole changes a user's role.
func (s *Store) SetUserRole(ctx context.Context, id uint, role Role) error {
	return s.mustAffect(ctx, s.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("role", role), "set user role")
}

// CreateSession persists a browser session.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionUser joins a session with its user and deletes expired sessions.
func (s *Store) SessionUser(ctx context.Context, id string) (Session, User, error) {
	var sess Session
	if err := s.db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now().UTC()).First(&sess).Error; err != nil {
		return Session{}, User{}, queryErr("load session", err)
	}
	u, err := s.UserByID(ctx, sess.UserID)
	if err != nil {
		return Session{}, User{}, err
	}
	return sess, u, nil
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Delete(&Session{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ListProjects returns every project ordered by slug.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := s.db.WithContext(ctx).Order("slug").Find(&projects).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

// ProjectBySlug loads a project by its slug.
func (s *Store) ProjectBySlug(ctx context.Context, slug string) (Project, error) {
	var p Project
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&p).Error
	return p, queryErr("load project", err)
}

// CreateProject inserts a project with its default environment.
func (s *Store) CreateProject(ctx context.Context, p Project, defaultEnv Environment) (Project, error) {
	err := s.Transaction(ctx, func(tx *Store) error {
		if err := tx.db.Create(&p).Error; err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		defaultEnv.ProjectID = p.ID
		if err := tx.db.Create(&defaultEnv).Error; err != nil {
			return fmt.Errorf("create default environment: %w", err)
		}
		return tx.setPrimaryEnvironment(ctx, p.ID, defaultEnv.ID)
	})
	if err != nil {
		return Project{}, err
	}
	return s.ProjectBySlug(ctx, p.Slug)
}

// UpdateProject changes display fields of a project.
func (s *Store) UpdateProject(ctx context.Context, id uint, name, description string) error {
	updates := map[string]any{"name": name, "description": description}
	return s.mustAffect(ctx, s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", id).Updates(updates), "update project")
}

// DeleteProject removes a project and everything scoped to it.
func (s *Store) DeleteProject(ctx context.Context, id uint) error {
	err := s.Transaction(ctx, func(tx *Store) error {
		if err := tx.removeProjectKeyScopes(ctx, id); err != nil {
			return err
		}
		scoped := []struct {
			model any
			query string
			arg   any
		}{
			{&Environment{}, "project_id = ?", id},
			{&ConfigItem{}, "project_id = ?", id},
			{&MCPProposal{}, "project_id = ?", id},
			{&Project{}, "id = ?", id},
		}
		for _, m := range scoped {
			if err := tx.db.Where(m.query, m.arg).Delete(m.model).Error; err != nil {
				return fmt.Errorf("delete scoped rows: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ListItems returns the items of one environment, optionally filtered by key prefix.
func (s *Store) ListItems(ctx context.Context, projectID, envID uint, keyPrefix string) ([]ConfigItem, error) {
	q := s.db.WithContext(ctx).Where("project_id = ? AND environment_id = ?", projectID, envID)
	if keyPrefix != "" {
		q = q.Where("`key` LIKE ?", escapeLike(keyPrefix)+"%")
	}
	var items []ConfigItem
	if err := q.Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return items, nil
}

// GetItem loads one item by key.
func (s *Store) GetItem(ctx context.Context, projectID, envID uint, key string) (ConfigItem, error) {
	var item ConfigItem
	err := s.db.WithContext(ctx).
		Where("project_id = ? AND environment_id = ? AND `key` = ?", projectID, envID, key).
		First(&item).Error
	return item, queryErr("load item", err)
}

func (s *Store) itemForUpdate(ctx context.Context, projectID, envID uint, key string) (ConfigItem, error) {
	var item ConfigItem
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND environment_id = ? AND `key` = ?", projectID, envID, key).
		First(&item).Error
	return item, queryErr("lock item", err)
}

// PutItem creates or replaces an item.
func (s *Store) PutItem(ctx context.Context, item ItemInput) (ConfigItem, error) {
	if err := s.PutItems(ctx, []ItemInput{item}); err != nil {
		return ConfigItem{}, err
	}
	return s.GetItem(ctx, item.ProjectID, item.EnvironmentID, item.Key)
}

// DeleteItem removes an item by key.
func (s *Store) DeleteItem(ctx context.Context, projectID, envID uint, key string) error {
	return s.Transaction(ctx, func(tx *Store) error {
		item, err := tx.itemForUpdate(ctx, projectID, envID, key)
		if err != nil {
			return err
		}
		if item.Secret {
			if err := tx.protectProposals(ctx, projectID, envID, key); err != nil {
				return err
			}
		}
		res := tx.db.Where("project_id = ? AND environment_id = ? AND `key` = ?", projectID, envID, key).Delete(&ConfigItem{})
		return tx.mustAffect(ctx, res, "delete item")
	})
}

// RenameItem changes the key of an existing item.
func (s *Store) RenameItem(ctx context.Context, projectID, envID uint, oldKey, newKey string) error {
	return s.Transaction(ctx, func(tx *Store) error {
		item, err := tx.itemForUpdate(ctx, projectID, envID, oldKey)
		if err != nil {
			return err
		}
		if item.Secret {
			for _, key := range []string{oldKey, newKey} {
				if err := tx.protectProposals(ctx, projectID, envID, key); err != nil {
					return err
				}
			}
		}
		res := tx.db.Model(&ConfigItem{}).
			Where("project_id = ? AND environment_id = ? AND `key` = ?", projectID, envID, oldKey).
			Update("key", newKey)
		return tx.mustAffect(ctx, res, "rename item")
	})
}

// CreateAPIKey stores a key digest.
func (s *Store) CreateAPIKey(ctx context.Context, k APIKey) (APIKey, error) {
	if err := s.db.WithContext(ctx).Create(&k).Error; err != nil {
		return APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

// ListAPIKeys returns keys without credential digests.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	var keys []APIKey
	if err := s.db.WithContext(ctx).Omit("digest").Order("id DESC").Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	return keys, nil
}

// APIKeyByDigest loads a key by its digest; the caller compares the project
// so an unknown project never reveals whether a key exists.
func (s *Store) APIKeyByDigest(ctx context.Context, digest [32]byte) (APIKey, error) {
	var k APIKey
	err := s.db.WithContext(ctx).Where("digest = ?", digest[:]).First(&k).Error
	return k, queryErr("load api key", err)
}

// TouchAPIKey records last use of a key; failures are not fatal to the request.
func (s *Store) TouchAPIKey(ctx context.Context, id uint) {
	now := time.Now().UTC()
	s.db.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).Update("last_used_at", &now)
}

// DeleteExpiredSessions drops sessions past their expiry.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Where("expires_at < ?", time.Now().UTC()).Delete(&Session{}).Error; err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}

// DeleteAPIKey revokes an authorized key.
func (s *Store) DeleteAPIKey(ctx context.Context, id uint, revision uint64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND revision = ?", id, revision).Delete(&APIKey{})
	return keyMutation(res, "delete api key")
}

// CreateProposal stores an agent proposal.
func (s *Store) CreateProposal(ctx context.Context, p MCPProposal) (MCPProposal, error) {
	err := s.Transaction(ctx, func(tx *Store) error {
		item, err := tx.itemForUpdate(ctx, p.ProjectID, p.EnvironmentID, p.Key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		p.Secret = p.Secret || item.Secret
		if err := tx.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Create(&p).Error; err != nil {
			return fmt.Errorf("create proposal: %w", err)
		}
		return nil
	})
	return p, err
}

// ProposalByID loads a proposal inside a project.
func (s *Store) ProposalByID(ctx context.Context, projectID, id uint) (MCPProposal, error) {
	var p MCPProposal
	err := s.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&p).Error
	return p, queryErr("load proposal", err)
}

// ListProposals returns proposals of a project filtered by status (empty = all).
func (s *Store) ListProposals(ctx context.Context, projectID uint, status string) ([]MCPProposal, error) {
	q := s.db.WithContext(ctx).Where("project_id = ?", projectID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var out []MCPProposal
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	return out, nil
}

// ReviewProposal applies or rejects a pending proposal atomically.
func (s *Store) ReviewProposal(ctx context.Context, projectID, id, reviewer uint, approve bool) (MCPProposal, error) {
	now := time.Now().UTC()
	var result MCPProposal
	err := s.Transaction(ctx, func(tx *Store) error {
		var p MCPProposal
		if err := tx.db.Where("project_id = ? AND id = ? AND status = 'pending'", projectID, id).First(&p).Error; err != nil {
			return queryErr("load pending proposal", err)
		}
		status := StatusRejected
		if approve {
			status = StatusApplied
			applyErr := tx.applyProposal(ctx, p, reviewer)
			if applyErr != nil {
				return applyErr
			}
		}
		res := tx.db.Model(&MCPProposal{}).Where("id = ? AND status = 'pending'", p.ID).
			Updates(map[string]any{"status": status, "reviewed_by": reviewer, "reviewed_at": now})
		if err := res.Error; err != nil {
			return fmt.Errorf("mark proposal: %w", err)
		}
		p.Status, p.ReviewedBy, p.ReviewedAt = status, &reviewer, &now
		result = p
		return nil
	})
	return result, err
}

func (s *Store) applyProposal(ctx context.Context, p MCPProposal, reviewer uint) error {
	switch p.Action {
	case ActionSet:
		if p.Value == nil {
			return ErrNotFound
		}
		item := ConfigItem{
			ProjectID: p.ProjectID, EnvironmentID: p.EnvironmentID, Key: p.Key,
			Value: *p.Value, Secret: p.Secret, UpdatedBy: reviewer,
		}
		columns := []string{"value", "updated_by", "updated_at"}
		if p.Secret {
			columns = append(columns, "secret")
			if err := s.protectProposals(ctx, p.ProjectID, p.EnvironmentID, p.Key); err != nil {
				return err
			}
		}
		err := s.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "environment_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns(columns),
		}).Create(&item).Error
		if err != nil {
			return fmt.Errorf("apply proposal set: %w", err)
		}
	case ActionDelete:
		if err := s.db.Where("project_id = ? AND environment_id = ? AND `key` = ?", p.ProjectID, p.EnvironmentID, p.Key).
			Delete(&ConfigItem{}).Error; err != nil {
			return fmt.Errorf("apply proposal delete: %w", err)
		}
	}
	return nil
}

func (s *Store) mustAffect(_ context.Context, res *gorm.DB, op string) error {
	if err := res.Error; err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return nil
}

func escapeLike(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%', '_', '\\':
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}
