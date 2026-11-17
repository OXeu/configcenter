package server

import (
	"crypto/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/store"
)

const mcpLoginCookie = "cc_mcp_login"

func (s *Server) beginMCPAuthorization(c *gin.Context, request store.MCPAuthorization) {
	id, loggedIn := auth.FromContext(c.Request.Context())
	if loggedIn && id.User != nil {
		request.UserID = id.User.ID
	}
	if err := s.store.CreateMCPAuthorization(c.Request.Context(), request); err != nil {
		s.fail(c, err)
		return
	}
	if request.UserID == 0 {
		s.startOAuth(c, request.ID)
		return
	}
	c.Redirect(http.StatusFound, "/oauth/consent?request="+url.QueryEscape(request.ID))
}

func (s *Server) mcpConsentRequest(c *gin.Context, requestID string) (store.MCPAuthorization, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	id, ok := auth.FromContext(c.Request.Context())
	if !ok || id.User == nil {
		oauthError(c, http.StatusUnauthorized, "login_required")
		return store.MCPAuthorization{}, false
	}
	request, err := s.store.MCPAuthorizationByID(c.Request.Context(), requestID, time.Now().UTC())
	if err != nil {
		s.mcpGrantError(c, err, "invalid_request")
		return request, false
	}
	if request.UserID != id.User.ID || request.Approved {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return request, false
	}
	return request, true
}

func (s *Server) mcpConsentPage(c *gin.Context) {
	request, ok := s.mcpConsentRequest(c, c.Query("request"))
	if !ok {
		return
	}
	client, err := s.store.MCPClientByID(c.Request.Context(), request.ClientID)
	if err != nil {
		s.mcpGrantError(c, err, "invalid_client")
		return
	}
	s.render(c, http.StatusOK, gin.H{
		"Title": "oauth.title", "Page": "oauth-consent",
		"Consent": gin.H{"request": request.ID, "clientName": client.Name, "redirectUri": request.RedirectURI, "resource": request.Resource},
	})
}

func (s *Server) mcpConsent(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		Request string `json:"request"`
		Approve *bool  `json:"approve"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Approve == nil {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	request, ok := s.mcpConsentRequest(c, input.Request)
	if !ok {
		return
	}
	var digest *string
	code, responseError := "", "access_denied"
	if *input.Approve {
		code, responseError = rand.Text(), ""
		hash := auth.TokenDigest(code)
		digest = &hash
	}
	if err := s.store.ApproveMCPAuthorization(c.Request.Context(), request.ID, request.UserID, digest, time.Now().UTC()); err != nil {
		s.mcpGrantError(c, err, "invalid_request")
		return
	}
	s.mcpAuthorizationRedirect(c, request, code, responseError, true)
}

func (s *Server) mcpAuthorizationRedirect(c *gin.Context, request store.MCPAuthorization, code, responseError string, jsonResponse bool) {
	redirect, err := url.Parse(request.RedirectURI)
	if err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	values := redirect.Query()
	values.Del("code")
	values.Del("error")
	values.Del("state")
	values.Set("iss", s.cfg.PublicURL)
	if request.State != "" {
		values.Set("state", request.State)
	}
	if code != "" {
		values.Set("code", code)
	} else {
		values.Set("error", responseError)
	}
	redirect.RawQuery = values.Encode()
	if jsonResponse {
		c.JSON(http.StatusOK, gin.H{"redirect": redirect.String()})
		return
	}
	c.Redirect(http.StatusFound, redirect.String())
}
