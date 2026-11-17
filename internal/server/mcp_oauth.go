package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/store"
)

func (s *Server) mountMCPOAuth(r *gin.Engine) {
	public := r.Group("", s.mcpOAuthPublic)
	public.GET("/.well-known/oauth-protected-resource", s.mcpResourceMetadata)
	public.GET("/.well-known/oauth-protected-resource/mcp", s.mcpResourceMetadata)
	public.GET("/.well-known/oauth-authorization-server", s.mcpAuthorizationMetadata)
	public.POST("/oauth/register", s.mcpRegister)
	public.POST("/oauth/token", s.mcpToken)
	public.POST("/oauth/revoke", s.mcpRevoke)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token", "/oauth/revoke"} {
		public.OPTIONS(path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}
}

func (s *Server) mcpOAuthPublic(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type")
	if s.oauth == nil {
		oauthError(c, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	c.Next()
}

func (s *Server) mcpResourceMetadata(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"resource": s.cfg.PublicURL + "/mcp", "resource_name": "ConfigCenter MCP",
		"authorization_servers": []string{s.cfg.PublicURL},
		"scopes_supported":      []string{"mcp"}, "bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) mcpAuthorizationMetadata(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                         s.cfg.PublicURL,
		"authorization_endpoint":                         s.cfg.PublicURL + "/oauth/authorize",
		"token_endpoint":                                 s.cfg.PublicURL + "/oauth/token",
		"registration_endpoint":                          s.cfg.PublicURL + "/oauth/register",
		"revocation_endpoint":                            s.cfg.PublicURL + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               []string{"mcp"},
		"authorization_response_iss_parameter_supported": true,
	})
}

type mcpRegistration struct {
	Name          string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	Scope         string   `json:"scope"`
}

func (s *Server) mcpRegister(c *gin.Context) {
	var input mcpRegistration
	if err := c.ShouldBindJSON(&input); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if len(input.RedirectURIs) == 0 || len(input.RedirectURIs) > 10 {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri")
		return
	}
	for _, uri := range input.RedirectURIs {
		if !validMCPRedirect(uri) {
			oauthError(c, http.StatusBadRequest, "invalid_redirect_uri")
			return
		}
	}
	if len(input.GrantTypes) == 0 {
		input.GrantTypes = []string{"authorization_code"}
	}
	if len(input.ResponseTypes) == 0 {
		input.ResponseTypes = []string{"code"}
	}
	validGrants := slices.Contains(input.GrantTypes, "authorization_code")
	for _, grant := range input.GrantTypes {
		validGrants = validGrants && (grant == "authorization_code" || grant == "refresh_token")
	}
	if !validGrants || len(input.ResponseTypes) != 1 || input.ResponseTypes[0] != "code" ||
		(input.AuthMethod != "" && input.AuthMethod != "none") || (input.Scope != "" && input.Scope != "mcp") || len(input.Name) > 128 {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = "MCP client"
	}
	client := store.MCPClient{ID: rand.Text(), Name: input.Name, RedirectURIs: input.RedirectURIs, Refresh: slices.Contains(input.GrantTypes, "refresh_token"), CreatedAt: time.Now().UTC()}
	if err := s.store.CreateMCPClient(c.Request.Context(), client); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"client_id": client.ID, "client_id_issued_at": client.CreatedAt.Unix(), "client_name": client.Name,
		"redirect_uris": client.RedirectURIs, "grant_types": input.GrantTypes, "response_types": input.ResponseTypes,
		"token_endpoint_auth_method": "none", "scope": "mcp",
	})
}

func validMCPRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "#*\\") || u.Opaque != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()))
}

func oauthError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code})
}

func (s *Server) mcpAuthorize(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if s.oauth == nil {
		oauthError(c, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || !singleOAuthValues(query) || len(c.Request.URL.RawQuery) > 8192 {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	client, err := s.store.MCPClientByID(c.Request.Context(), query.Get("client_id"))
	if err != nil {
		s.mcpGrantError(c, err, "invalid_client")
		return
	}
	if !slices.Contains(client.RedirectURIs, query.Get("redirect_uri")) {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	request := store.MCPAuthorization{
		ID: rand.Text(), ClientID: client.ID, RedirectURI: query.Get("redirect_uri"), State: query.Get("state"),
		Resource: query.Get("resource"), Challenge: query.Get("code_challenge"), ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	code := ""
	challenge, decodeErr := base64.RawURLEncoding.DecodeString(request.Challenge)
	switch {
	case query.Get("response_type") != "code":
		code = "unsupported_response_type"
	case query.Get("scope") != "" && query.Get("scope") != "mcp":
		code = "invalid_scope"
	case request.Resource != s.cfg.PublicURL+"/mcp":
		code = "invalid_target"
	case query.Get("code_challenge_method") != "S256" || decodeErr != nil || len(challenge) != 32 || len(request.Challenge) != 43 || len(request.State) > 2048:
		code = "invalid_request"
	}
	if code != "" {
		s.mcpAuthorizationRedirect(c, request, "", code, false)
		return
	}
	s.beginMCPAuthorization(c, request)
}

func singleOAuthValues(values url.Values) bool {
	for _, entries := range values {
		if len(entries) != 1 {
			return false
		}
	}
	return true
}

func (s *Server) mcpGrantError(c *gin.Context, err error, code string) {
	if errors.Is(err, store.ErrNotFound) {
		oauthError(c, http.StatusBadRequest, code)
		return
	}
	s.log.Error("MCP OAuth failed", "error", err)
	oauthError(c, http.StatusInternalServerError, "server_error")
}
