package store

import "time"

// Role is the permission level of a user.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

// CanWrite reports whether the role may modify projects.
func (r Role) CanWrite() bool {
	return r == RoleUser || r == RoleAdmin
}

// User is a human account, optionally linked to an OAuth subject.
type User struct {
	ID          uint   `gorm:"primaryKey"`
	Email       string `gorm:"uniqueIndex;size:254;not null"`
	Name        string `gorm:"not null"`
	Role        Role   `gorm:"not null"`
	OAuthIssuer string `gorm:"column:oauth_issuer;index"`
	AvatarURL   string
	OAuthSub    string     `gorm:"column:oauth_sub;index"`
	CreatedAt   time.Time  `gorm:"not null"`
	LastLoginAt *time.Time `gorm:""`
}

// Session is a browser login session.
type Session struct {
	ID        string `gorm:"primaryKey;size:64"`
	UserID    uint   `gorm:"index;not null"`
	CSRFToken string `gorm:"not null"`
	CreatedAt time.Time
	ExpiresAt time.Time `gorm:"index;not null"`
}

// Project is the isolation boundary for every configuration item.
type Project struct {
	OwnerID     uint   `gorm:"index;not null;default:0"`
	ID          uint   `gorm:"primaryKey"`
	Slug        string `gorm:"uniqueIndex;size:64;not null"`
	Name        string `gorm:"not null"`
	Description string
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

// Environment is a named deployment target inside a project.
type Environment struct {
	ID        uint      `gorm:"primaryKey"`
	ProjectID uint      `gorm:"uniqueIndex:env_scope;not null"`
	Slug      string    `gorm:"uniqueIndex:env_scope;size:64;not null"`
	Name      string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
}

// ConfigItem is one key/value pair scoped to project and environment.
type ConfigItem struct {
	ID            uint   `gorm:"primaryKey"`
	ProjectID     uint   `gorm:"uniqueIndex:item_scope;not null"`
	EnvironmentID uint   `gorm:"uniqueIndex:item_scope;not null"`
	Key           string `gorm:"uniqueIndex:item_scope;size:191;not null"`
	Value         string `gorm:"type:longtext;not null"`
	Secret        bool   `gorm:"not null;default:false"`
	Description   string
	UpdatedBy     uint      `gorm:"not null"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

// MCPProposal is an agent-submitted change waiting for human review.
type MCPProposal struct {
	ID            uint    `gorm:"primaryKey"`
	ProjectID     uint    `gorm:"index;not null"`
	EnvironmentID uint    `gorm:"not null"`
	Key           string  `gorm:"size:191;not null"`
	Value         *string `gorm:"type:text"`
	Secret        bool    `gorm:"not null;default:false"`
	Action        string  `gorm:"not null"`
	Reason        string
	Status        string    `gorm:"index;not null"`
	ProposedBy    uint      `gorm:"not null"`
	APIKeyID      uint      `gorm:"index;not null"`
	ReviewedBy    *uint     `gorm:""`
	CreatedAt     time.Time `gorm:"not null"`
	ReviewedAt    *time.Time
}

// APIKey authenticates a client with explicitly granted project permissions.
type APIKey struct {
	ID         uint       `gorm:"primaryKey"`
	Scopes     []KeyScope `gorm:"serializer:json;type:text"`
	Revision   uint64     `gorm:"not null;default:1"`
	Name       string     `gorm:"not null"`
	Digest     []byte     `gorm:"uniqueIndex;size:32;not null"`
	Prefix     string     `gorm:"not null"`
	CreatedBy  uint       `gorm:"not null"`
	CreatedAt  time.Time  `gorm:"not null"`
	LastUsedAt *time.Time `gorm:""`
	ExpiresAt  *time.Time `gorm:""`
}

// Proposal statuses.
const (
	StatusPending  = "pending"
	StatusApplied  = "applied"
	StatusRejected = "rejected"
)

// Proposal actions.
const (
	ActionSet    = "set"
	ActionDelete = "delete"
)

// Identity is the normalized OAuth profile of a logging-in user.
type Identity struct {
	Issuer    string
	Sub       string
	Email     string
	Name      string
	AvatarURL string
	Role      Role
	Now       any
}
