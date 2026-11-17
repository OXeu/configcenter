package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/apikey"
	"github.com/OXeu/configcenter/internal/store"
)

// ActorID returns the account responsible for a session or API key operation.
func (i Identity) ActorID() uint {
	if i.User != nil {
		return i.User.ID
	}
	if i.APIKey != nil {
		return i.APIKey.CreatedBy
	}
	return 0
}

func (a *Authenticator) requestKey(c *gin.Context) (store.APIKey, bool) {
	token, err := apikey.ParseHeader(c.GetHeader("Authorization"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
		return store.APIKey{}, false
	}
	key, err := a.store.APIKeyByDigest(c.Request.Context(), apikey.Hash(token))
	if err != nil {
		a.unauthorised(c, err)
		return store.APIKey{}, false
	}
	if apikeyExpired(key, a.now()) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key expired"})
		return store.APIKey{}, false
	}
	return key, true
}

// ManagementMiddleware resolves a browser user or an API key before scope checks.
func (a *Authenticator) ManagementMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, ok := FromContext(c.Request.Context()); ok && id.MCP && id.User != nil {
			c.Next()
			return
		}
		if c.GetHeader("Authorization") == "" {
			RequireUser()(c)
			return
		}
		key, ok := a.requestKey(c)
		if !ok {
			return
		}
		a.store.TouchAPIKey(c.Request.Context(), key.ID)
		c.Writer.Header().Del("WWW-Authenticate")
		c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), Identity{APIKey: &key}))
		c.Next()
	}
}

// Allows checks explicit API scopes or the browser user's operation role.
func (i Identity) Allows(projectID uint, permission store.Permission) bool {
	if i.APIKey != nil {
		return i.APIKey.Allows(projectID, permission)
	}
	return i.User != nil && i.Role.CanWrite() && (!permission.GlobalOnly() || i.Role == store.RoleAdmin)
}

// RequireScope checks an operation against the resolved project, or all projects when absent.
func RequireScope(permission store.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := FromContext(c.Request.Context())
		var projectID uint
		if id.Project != nil {
			projectID = id.Project.ID
		}
		if !ok || !id.Allows(projectID, permission) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient api key scope"})
			return
		}
		c.Next()
	}
}
