package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/store"
)

func oauthForm(c *gin.Context) (url.Values, bool) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	if err := c.Request.ParseForm(); err != nil || !singleOAuthValues(c.Request.PostForm) {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	form := c.Request.PostForm
	if c.GetHeader("Authorization") != "" || form.Get("client_secret") != "" || form.Get("client_id") == "" {
		oauthError(c, http.StatusBadRequest, "invalid_client")
		return nil, false
	}
	return form, true
}

func (s *Server) mcpToken(c *gin.Context) {
	form, ok := oauthForm(c)
	if !ok {
		return
	}
	client, err := s.store.MCPClientByID(c.Request.Context(), form.Get("client_id"))
	if err != nil {
		s.mcpGrantError(c, err, "invalid_client")
		return
	}
	if form.Get("resource") != s.cfg.PublicURL+"/mcp" {
		oauthError(c, http.StatusBadRequest, "invalid_target")
		return
	}
	if form.Get("scope") != "" && form.Get("scope") != "mcp" {
		oauthError(c, http.StatusBadRequest, "invalid_scope")
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		s.mcpExchangeCode(c, client, form)
	case "refresh_token":
		if !client.Refresh {
			oauthError(c, http.StatusBadRequest, "unauthorized_client")
			return
		}
		s.mcpRefresh(c, client, form)
	default:
		oauthError(c, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (s *Server) mcpExchangeCode(c *gin.Context, client store.MCPClient, form url.Values) {
	now := time.Now().UTC()
	request, err := s.store.MCPAuthorizationByCode(c.Request.Context(), auth.TokenDigest(form.Get("code")), now)
	if err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	if request.ClientID != client.ID || request.RedirectURI != form.Get("redirect_uri") || request.Resource != form.Get("resource") || !validPKCE(form.Get("code_verifier"), request.Challenge) {
		oauthError(c, http.StatusBadRequest, "invalid_grant")
		return
	}
	if _, err := s.store.UserByID(c.Request.Context(), request.UserID); err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	token, response := newMCPToken(request.ID, client.Refresh, now)
	if err := s.store.ExchangeMCPCode(c.Request.Context(), request, token, now); err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	c.JSON(http.StatusOK, response)
}

func validPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, char := range verifier {
		valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~", char)
		if !valid {
			return false
		}
	}
	digest := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(digest[:])), []byte(challenge)) == 1
}

func newMCPToken(grantID string, refresh bool, now time.Time) (store.MCPToken, gin.H) {
	accessToken := auth.MCPAccessPrefix + rand.Text()
	refreshToken := "cc_mcp_refresh_" + rand.Text()
	token := store.MCPToken{AccessDigest: auth.TokenDigest(accessToken), RefreshDigest: auth.TokenDigest(refreshToken), GrantID: grantID, ExpiresAt: now.Add(time.Hour)}
	response := gin.H{"access_token": accessToken, "token_type": "Bearer", "expires_in": 3600, "scope": "mcp"}
	if refresh {
		response["refresh_token"] = refreshToken
	}
	return token, response
}

func (s *Server) mcpRefresh(c *gin.Context, client store.MCPClient, form url.Values) {
	now := time.Now().UTC()
	previous, err := s.store.MCPRefreshToken(c.Request.Context(), auth.TokenDigest(form.Get("refresh_token")))
	if err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	grant, err := s.store.MCPGrantByID(c.Request.Context(), previous.GrantID, now)
	if err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	if grant.ClientID != client.ID || grant.Resource != form.Get("resource") {
		oauthError(c, http.StatusBadRequest, "invalid_grant")
		return
	}
	if _, err := s.store.UserByID(c.Request.Context(), grant.UserID); err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	next, response := newMCPToken(grant.ID, true, now)
	if next.ExpiresAt.After(grant.ExpiresAt) {
		next.ExpiresAt = grant.ExpiresAt
		response["expires_in"] = int(grant.ExpiresAt.Sub(now).Seconds())
	}
	if err := s.store.RotateMCPToken(c.Request.Context(), previous, next, now); err != nil {
		s.mcpGrantError(c, err, "invalid_grant")
		return
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) mcpRevoke(c *gin.Context) {
	form, ok := oauthForm(c)
	if !ok {
		return
	}
	if form.Get("token") == "" {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	grant, err := s.store.MCPGrantByToken(c.Request.Context(), auth.TokenDigest(form.Get("token")), form.Get("client_id"))
	if errors.Is(err, store.ErrNotFound) {
		c.Status(http.StatusOK)
		return
	}
	if err != nil {
		s.mcpGrantError(c, err, "invalid_request")
		return
	}
	if err := s.store.RevokeMCPGrant(c.Request.Context(), grant.ID); err != nil {
		s.mcpGrantError(c, err, "invalid_request")
		return
	}
	c.Status(http.StatusOK)
}
