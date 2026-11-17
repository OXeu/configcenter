package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

// MCPClient is a registered public OAuth client.
type MCPClient struct {
	ID           string   `gorm:"primaryKey;size:64"`
	Name         string   `gorm:"size:128;not null"`
	RedirectURIs []string `gorm:"serializer:json;type:text;not null"`
	Refresh      bool
	CreatedAt    time.Time
}

// MCPAuthorization binds a browser approval and a single-use PKCE code.
type MCPAuthorization struct {
	ID          string `gorm:"primaryKey;size:64"`
	ClientID    string `gorm:"size:64;not null"`
	UserID      uint   `gorm:"index;not null"`
	RedirectURI string
	State       string
	Resource    string
	Challenge   string  `gorm:"size:43"`
	LoginDigest string  `gorm:"size:64"`
	CodeDigest  *string `gorm:"uniqueIndex;size:64"`
	Approved    bool
	CodeUsed    bool
	ExpiresAt   time.Time `gorm:"index;not null"`
}

// MCPGrant is the revocable lifetime of an OAuth authorization.
type MCPGrant struct {
	ID        string `gorm:"primaryKey;size:64"`
	ClientID  string `gorm:"size:64;not null"`
	UserID    uint   `gorm:"index;not null"`
	Resource  string
	Revoked   bool
	ExpiresAt time.Time `gorm:"index;not null"`
}

// MCPToken stores only digests and retains refresh history for replay detection.
type MCPToken struct {
	AccessDigest  string `gorm:"primaryKey;size:64"`
	RefreshDigest string `gorm:"uniqueIndex;size:64;not null"`
	GrantID       string `gorm:"index;size:64;not null"`
	Used          bool
	ExpiresAt     time.Time `gorm:"not null"`
}

// CreateMCPClient registers a public client.
func (s *Store) CreateMCPClient(ctx context.Context, client MCPClient) error {
	return queryErr("create MCP client", s.db.WithContext(ctx).Create(&client).Error)
}

// MCPClientByID loads registered redirect URIs.
func (s *Store) MCPClientByID(ctx context.Context, id string) (MCPClient, error) {
	var client MCPClient
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&client).Error
	return client, queryErr("load MCP client", err)
}

// CreateMCPAuthorization records a short-lived authorization request.
func (s *Store) CreateMCPAuthorization(ctx context.Context, request MCPAuthorization) error {
	return queryErr("create MCP authorization", s.db.WithContext(ctx).Create(&request).Error)
}

// MCPAuthorizationByID loads an unexpired browser request.
func (s *Store) MCPAuthorizationByID(ctx context.Context, id string, now time.Time) (MCPAuthorization, error) {
	var request MCPAuthorization
	err := s.db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, now).First(&request).Error
	return request, queryErr("load MCP authorization", err)
}

// BindMCPLogin binds an upstream login state to one pending authorization.
func (s *Store) BindMCPLogin(ctx context.Context, id, digest string) error {
	result := s.db.WithContext(ctx).Model(&MCPAuthorization{}).Where("id = ? AND user_id = 0", id).Update("login_digest", digest)
	return changedMCPRow("bind MCP login", result.Error, result.RowsAffected)
}

// CompleteMCPLogin binds a verified upstream identity to the pending approval.
func (s *Store) CompleteMCPLogin(ctx context.Context, id, digest string, userID uint, now time.Time) error {
	result := s.db.WithContext(ctx).Model(&MCPAuthorization{}).
		Where("id = ? AND login_digest = ? AND user_id = 0 AND expires_at > ?", id, digest, now).
		Updates(map[string]any{"user_id": userID, "login_digest": ""})
	return changedMCPRow("complete MCP login", result.Error, result.RowsAffected)
}

// ApproveMCPAuthorization consumes consent once and issues a short-lived code.
func (s *Store) ApproveMCPAuthorization(ctx context.Context, id string, userID uint, digest *string, now time.Time) error {
	result := s.db.WithContext(ctx).Model(&MCPAuthorization{}).
		Where("id = ? AND user_id = ? AND approved = ? AND expires_at > ?", id, userID, false, now).
		Updates(map[string]any{"approved": true, "code_digest": digest, "expires_at": now.Add(5 * time.Minute)})
	return changedMCPRow("approve MCP authorization", result.Error, result.RowsAffected)
}

// MCPAuthorizationByCode loads an approved code, including consumed codes.
func (s *Store) MCPAuthorizationByCode(ctx context.Context, digest string, now time.Time) (MCPAuthorization, error) {
	var request MCPAuthorization
	err := s.db.WithContext(ctx).Where("code_digest = ? AND approved = ? AND expires_at > ?", digest, true, now).First(&request).Error
	return request, queryErr("load MCP code", err)
}

// ExchangeMCPCode atomically consumes a code and creates the token family.
func (s *Store) ExchangeMCPCode(ctx context.Context, request MCPAuthorization, token MCPToken, now time.Time) error {
	replayed := false
	err := s.Transaction(ctx, func(tx *Store) error {
		result := tx.db.WithContext(ctx).Model(&MCPAuthorization{}).
			Where("id = ? AND code_used = ? AND expires_at > ?", request.ID, false, now).Update("code_used", true)
		if result.Error != nil {
			return fmt.Errorf("consume MCP code: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			replayed = true
			return tx.RevokeMCPGrant(ctx, request.ID)
		}
		grant := MCPGrant{ID: request.ID, ClientID: request.ClientID, UserID: request.UserID, Resource: request.Resource, ExpiresAt: now.Add(30 * 24 * time.Hour)}
		if err := tx.db.WithContext(ctx).Create(&grant).Error; err != nil {
			return fmt.Errorf("create MCP grant: %w", err)
		}
		return tx.createMCPToken(ctx, token)
	})
	if err != nil {
		return err
	}
	if replayed {
		return ErrNotFound
	}
	return nil
}

func (s *Store) createMCPToken(ctx context.Context, token MCPToken) error {
	return queryErr("create MCP token", s.db.WithContext(ctx).Create(&token).Error)
}

// MCPRefreshToken loads a refresh credential, including used credentials.
func (s *Store) MCPRefreshToken(ctx context.Context, digest string) (MCPToken, error) {
	var token MCPToken
	err := s.db.WithContext(ctx).Where("refresh_digest = ?", digest).First(&token).Error
	return token, queryErr("load MCP refresh token", err)
}

// MCPGrantByID loads a live token family.
func (s *Store) MCPGrantByID(ctx context.Context, id string, now time.Time) (MCPGrant, error) {
	var grant MCPGrant
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND revoked = ? AND expires_at > ?", id, false, now).First(&grant).Error
	return grant, queryErr("load MCP grant", err)
}

// RotateMCPToken replaces a refresh credential, revoking the family on reuse.
func (s *Store) RotateMCPToken(ctx context.Context, previous, next MCPToken, now time.Time) error {
	replayed := false
	err := s.Transaction(ctx, func(tx *Store) error {
		if _, err := tx.MCPGrantByID(ctx, previous.GrantID, now); err != nil {
			return err
		}
		result := tx.db.WithContext(ctx).Model(&MCPToken{}).
			Where("access_digest = ? AND used = ?", previous.AccessDigest, false).Update("used", true)
		if result.Error != nil {
			return fmt.Errorf("consume MCP refresh token: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			replayed = true
			return tx.RevokeMCPGrant(ctx, previous.GrantID)
		}
		return tx.createMCPToken(ctx, next)
	})
	if err != nil {
		return err
	}
	if replayed {
		return ErrNotFound
	}
	return nil
}

// RevokeMCPGrant invalidates all access and refresh tokens of an authorization.
func (s *Store) RevokeMCPGrant(ctx context.Context, id string) error {
	return queryErr("revoke MCP grant", s.db.WithContext(ctx).Model(&MCPGrant{}).Where("id = ?", id).Update("revoked", true).Error)
}

func (s *Store) revokeMCPUserGrants(ctx context.Context, userID uint) error {
	return queryErr("revoke user MCP grants", s.db.WithContext(ctx).Model(&MCPGrant{}).Where("user_id = ?", userID).Update("revoked", true).Error)
}

// MCPGrantByToken resolves either credential for client-bound revocation.
func (s *Store) MCPGrantByToken(ctx context.Context, digest, clientID string) (MCPGrant, error) {
	var grant MCPGrant
	err := s.db.WithContext(ctx).Model(&MCPGrant{}).Select("mcp_grants.*").
		Joins("JOIN mcp_tokens ON mcp_tokens.grant_id = mcp_grants.id").
		Where("(mcp_tokens.access_digest = ? OR mcp_tokens.refresh_digest = ?) AND mcp_grants.client_id = ?", digest, digest, clientID).
		First(&grant).Error
	return grant, queryErr("load MCP revocation grant", err)
}

// MCPTokenUser validates audience and expiry and loads the user's current role.
func (s *Store) MCPTokenUser(ctx context.Context, digest, resource string, now time.Time) (User, error) {
	var user User
	err := s.db.WithContext(ctx).Model(&User{}).Select("users.*").
		Joins("JOIN mcp_grants ON mcp_grants.user_id = users.id").
		Joins("JOIN mcp_tokens ON mcp_tokens.grant_id = mcp_grants.id").
		Where("mcp_tokens.access_digest = ? AND mcp_tokens.used = ? AND mcp_tokens.expires_at > ?", digest, false, now).
		Where("mcp_grants.resource = ? AND mcp_grants.revoked = ? AND mcp_grants.expires_at > ?", resource, false, now).
		First(&user).Error
	return user, queryErr("authenticate MCP token", err)
}

func changedMCPRow(op string, err error, rows int64) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return nil
}
