package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/store"
)

const oauthStateCookie = "cc_oauth_state"

// MountOAuth registers the login, callback and logout routes.
func (s *Server) MountOAuth(r *gin.Engine) {
	r.GET("/login", s.loginPage)
	r.GET("/auth/login", s.oauthStart)
	r.GET("/oauth/callback", s.oauthCallback)
	r.POST("/logout", s.logout)
	r.GET("/oauth/authorize", s.mcpAuthorize)
	r.GET("/oauth/consent", s.mcpConsentPage)
	r.POST("/oauth/consent", s.mcpConsent)
}

func (s *Server) loginPage(c *gin.Context) {
	if id, ok := auth.FromContext(c.Request.Context()); ok && id.User != nil {
		c.Redirect(http.StatusFound, "/projects")
		return
	}
	s.render(c, http.StatusOK, gin.H{
		"Title":       "login.title",
		"LoginFailed": s.loginError(c),
	})
}

func (s *Server) providerName() string {
	if s.oauth == nil {
		return "single sign-on"
	}
	if d, ok := s.oauth.(interface{ Name() string }); ok {
		return d.Name()
	}
	return "identity provider"
}

func (s *Server) oauthStart(c *gin.Context) {
	s.startOAuth(c, "")
}

func (s *Server) startOAuth(c *gin.Context, requestID string) {
	c.Header("Cache-Control", "no-store")
	clearCookie(c, mcpLoginCookie, "/oauth")
	if s.oauth == nil {
		s.redirectWithError(c, "oauth_disabled")
		return
	}
	redirectURL, stateBlob, err := s.oauth.Start(c.Request.Context())
	if err != nil {
		s.redirectWithError(c, "oauth_unavailable")
		return
	}
	if requestID != "" {
		if err := s.store.BindMCPLogin(c.Request.Context(), requestID, auth.TokenDigest(stateBlob)); err != nil {
			s.redirectWithError(c, "oauth_failed")
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name: mcpLoginCookie, Value: requestID, Path: "/oauth", HttpOnly: true,
			Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600,
		})
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    stateBlob,
		Path:     "/oauth/callback",
		HttpOnly: true,
		Secure:   c.Request.TLS != nil || strings.HasPrefix(s.cfg.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
	c.Redirect(http.StatusFound, redirectURL)
}

func (s *Server) oauthCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	clearCookie(c, oauthStateCookie, "/oauth/callback")
	clearCookie(c, mcpLoginCookie, "/oauth")
	if s.oauth == nil {
		s.redirectWithError(c, "oauth_disabled")
		return
	}
	stateBlob, err := c.Cookie(oauthStateCookie)
	if err != nil {
		s.redirectWithError(c, "missing_state")
		return
	}
	if c.Query("error") != "" {
		s.redirectWithError(c, "denied")
		return
	}
	claims, err := s.oauth.Finish(c.Request.Context(), c.Query("code"), c.Query("state"), stateBlob)
	if err != nil {
		s.log.Error("oauth callback failed", "error", err)
		s.redirectWithError(c, "oauth_failed")
		return
	}
	identity, err := s.identityFromClaims(claims)
	if err != nil {
		s.redirectWithError(c, "no_account")
		return
	}
	user, err := s.store.LinkOAuthUser(c.Request.Context(), identity)
	if errors.Is(err, store.ErrRegistrationClosed) {
		s.redirectWithError(c, "registration_closed")
		return
	}
	if err != nil {
		s.redirectWithError(c, "no_account")
		return
	}
	if err := s.authenticator.Login(c, user.ID); err != nil {
		s.redirectWithError(c, "session_failed")
		return
	}
	if requestID, cookieErr := c.Cookie(mcpLoginCookie); cookieErr == nil && requestID != "" {
		if err := s.store.CompleteMCPLogin(c.Request.Context(), requestID, auth.TokenDigest(stateBlob), user.ID, time.Now().UTC()); err != nil {
			s.redirectWithError(c, "oauth_failed")
			return
		}
		c.Redirect(http.StatusFound, "/oauth/consent?request="+url.QueryEscape(requestID))
		return
	}
	c.Redirect(http.StatusFound, "/projects")
}

func (s *Server) identityFromClaims(claims auth.Claims) (store.Identity, error) {
	role := s.decideRole(claims)
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	issuer := strings.TrimRight(claims.Issuer, "/")
	if claims.Sub == "" || issuer == "" {
		return store.Identity{}, fmt.Errorf("claims incomplete: %w", errNoAccount)
	}
	name := claims.Name
	if email == "" || strings.HasSuffix(email, "@users.noreply.github.com") {
		// Providers without a public email (GitHub) yield GitHub's no-reply
		// address or none; an issuer-scoped address keeps the account stable.
		email = claims.Sub + "@" + issuerHost(issuer)
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	return store.Identity{
		Issuer:    issuer,
		Sub:       claims.Sub,
		Email:     email,
		Name:      name,
		AvatarURL: claims.Picture,
		Role:      role,
		Now:       time.Now().UTC(),
	}, nil
}

func issuerHost(issuer string) string {
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" {
		return "users.invalid"
	}
	return u.Host
}

func (s *Server) decideRole(claims auth.Claims) store.Role {
	if s.groupPolicy.AdminIn(claims.Groups) {
		return store.RoleAdmin
	}
	return s.groupPolicy.RoleFor(claims.Groups)
}

func (s *Server) logout(c *gin.Context) {
	if err := s.authenticator.Logout(c); err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	c.Redirect(http.StatusFound, "/login")
}

func clearCookie(c *gin.Context, name, path string) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

var errNoAccount = errors.New("oauth principal has no usable account")
