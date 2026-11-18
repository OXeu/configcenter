package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/OXeu/center-common/oauth"
	"github.com/OXeu/configcenter/internal/store"
)

// Provider abstracts OIDC and plain OAuth 2.0 identity servers.
type Provider interface {
	Name() string
	// Start returns the authorization URL and an opaque blob to keep in the
	// browser until the callback arrives.
	Start(ctx context.Context) (redirectURL, state string, err error)
	// Finish validates the callback and returns the verified claims.
	Finish(ctx context.Context, code, state, stateBlob string) (Claims, error)
}

// NewProvider builds the configured provider without contacting the issuer.
// Discovery runs on login requests; an unconfigured provider returns nil.
func NewProvider(cfg Config) Provider {
	if !cfg.Enabled() {
		return nil
	}
	client := oauth.New(oauth.Config{
		IssuerURL:    cfg.IssuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scopes:       cfg.Scopes,
		RedirectURL:  cfg.RedirectURL,
		GroupsClaim:  cfg.GroupsClaim,
	}, nil)
	return adapter{client}
}

type adapter struct{ *oauth.Client }

// Start begins the authorization-code flow upstream.
func (a adapter) Start(ctx context.Context) (redirectURL, state string, err error) {
	redirectURL, state, err = a.Client.Start(ctx)
	if err != nil {
		return "", "", fmt.Errorf("provider start: %w", err)
	}
	return redirectURL, state, nil
}

// Finish completes the callbacks and verifies the claims.
func (a adapter) Finish(ctx context.Context, code, state, blob string) (Claims, error) {
	c, err := a.Client.Finish(ctx, code, state, blob)
	if err != nil {
		return Claims{}, fmt.Errorf("provider finish: %w", err)
	}
	return Claims(c), nil
}

// Claims is the verified identity returned by any OAuth provider.
type Claims oauth.Claims

// GroupPolicy maps OAuth groups onto roles.
type GroupPolicy struct {
	AdminGroups []string
	Mapping     map[string]string
}

// AdminIn reports whether any group grants the admin role.
func (p GroupPolicy) AdminIn(groups []string) bool {
	for _, g := range groups {
		for _, admin := range p.AdminGroups {
			if strings.EqualFold(g, admin) {
				return true
			}
		}
	}
	return false
}

// RoleFor returns the highest mapped role, defaulting to user.
func (p GroupPolicy) RoleFor(groups []string) store.Role {
	best := store.RoleUser
	for _, g := range groups {
		candidate := store.Role(p.Mapping[g])
		if candidate == store.RoleAdmin {
			best = candidate
		}
	}
	return best
}
