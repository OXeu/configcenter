package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/apikey"
	"github.com/OXeu/configcenter/internal/store"
)

// MCPAccessPrefix distinguishes MCP-only OAuth tokens from API keys.
const MCPAccessPrefix = "cc_mcp_access_"

// TokenDigest returns the non-secret representation used for OAuth credentials.
func TokenDigest(token string) string {
	return fmt.Sprintf("%x", apikey.Hash(token))
}

// MCPMiddleware accepts API keys, browser sessions and audience-bound OAuth tokens.
func (a *Authenticator) MCPMiddleware(resource, metadataURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		challenge := "Bearer"
		if metadataURL != "" {
			challenge += fmt.Sprintf(" resource_metadata=%q, scope=%q", metadataURL, "mcp")
		}
		c.Header("WWW-Authenticate", challenge)
		header := c.GetHeader("Authorization")
		if header == "" {
			id, ok := FromContext(c.Request.Context())
			if !ok || id.User == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization required"})
				return
			}
			c.Writer.Header().Del("WWW-Authenticate")
			c.Next()
			return
		}
		token, err := apikey.ParseHeader(header)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
			return
		}
		if !strings.HasPrefix(token, MCPAccessPrefix) {
			a.ManagementMiddleware()(c)
			return
		}
		user, err := a.store.MCPTokenUser(c.Request.Context(), TokenDigest(token), resource, a.now())
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.Header("WWW-Authenticate", challenge+`, error="invalid_token"`)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired access token"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.Writer.Header().Del("WWW-Authenticate")
		c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), Identity{User: &user, Role: user.Role, MCP: true}))
		c.Next()
	}
}

func (a *Authenticator) mcpProject(c *gin.Context, id Identity) {
	if c.Query("project") == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "project is required"})
		return
	}
	project, err := a.store.ProjectBySlug(c.Request.Context(), c.Query("project"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if id.Role != store.RoleAdmin && project.OwnerID != id.ActorID() {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "project access required"})
		return
	}
	id.Project = &project
	c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), id))
	c.Next()
}
