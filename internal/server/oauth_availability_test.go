package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/OXeu/configcenter/internal/apikey"
	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/config"
	"github.com/OXeu/configcenter/internal/store"
)

func TestOAuthOutageDoesNotBlockStartupAndRecoversOnLogin(t *testing.T) {
	idp := newFakeIdP(t)
	idp.discoveryStatus.Store(http.StatusServiceUnavailable)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	cfg := config.Config{
		DatabaseDSN: "file:oauth-outage-" + rand.Text() + "?mode=memory&cache=shared",
		PublicURL:   "http://cc.test",
		SessionTTL:  time.Hour,
		Languages:   []string{"en", "zh-CN"},
		Bootstrap:   config.Bootstrap{AdminEmail: "admin@localhost", AdminName: "admin"},
		OAuth: auth.Config{
			IssuerURL: idp.server.URL, ClientID: "client", ClientSecret: "client-secret-not-for-logs",
			RedirectURL: "http://cc.test/oauth/callback", GroupsClaim: "groups",
		},
	}
	srv := newTestServer(t, cfg, logger)
	router := srv.router()
	if idp.discoveryRequests.Load() != 0 {
		t.Fatal("startup contacted the OAuth provider")
	}
	admin, err := srv.store.UserByEmail(t.Context(), "admin@localhost")
	if err != nil {
		t.Fatal(err)
	}
	session := mcpUserSession(t, srv, router, admin.ID)
	if status, _ := session.do(http.MethodGet, "/api/v1/projects", nil); status != http.StatusOK {
		t.Fatal("provider outage blocked an existing session")
	}
	project, err := srv.store.ProjectBySlug(t.Context(), "configcenter")
	if err != nil {
		t.Fatal(err)
	}
	token, digest, err := apikey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateAPIKey(t.Context(), store.APIKey{
		Name: "reader", Digest: digest[:], CreatedBy: admin.ID,
		Scopes: []store.KeyScope{{ProjectID: project.ID, Permissions: []store.Permission{store.ProjectsRead, store.EnvironmentsRead, store.ConfigRead}}},
	}); err != nil {
		t.Fatal(err)
	}
	keyRequest(t, router, token, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusOK)
	anonymous := &flows{t: t, router: router, cookies: map[string]*http.Cookie{}}
	if status, _ := anonymous.do(http.MethodGet, "/api/v1/projects", nil); status != http.StatusUnauthorized {
		t.Fatal("provider outage bypassed authentication")
	}
	if idp.discoveryRequests.Load() != 0 {
		t.Fatal("existing sessions or API keys contacted the OAuth provider")
	}
	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/auth/login", http.NoBody))
	if failed.Code != http.StatusFound || failed.Header().Get("Location") != "/login?error=oauth_unavailable" {
		t.Fatal("provider outage did not report a retryable login error")
	}
	if !strings.Contains(logs.String(), "oauth provider unavailable") || strings.Contains(logs.String(), cfg.OAuth.ClientSecret) || strings.Contains(logs.String(), "private-provider-response") {
		t.Fatal("provider outage was not safely logged")
	}
	for language, message := range map[string]string{
		"en":    "The identity provider is temporarily unavailable. Please try again later.",
		"zh-CN": "身份提供方暂时不可用，请稍后重试。",
	} {
		page := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, failed.Header().Get("Location")+"&lang="+language, http.NoBody)
		request.Header.Set("X-Center-Navigation", "1")
		router.ServeHTTP(page, request)
		var body struct {
			Data struct {
				OAuthEnabled bool                     `json:"oauthEnabled"`
				Flash        struct{ Message string } `json:"flash"`
			} `json:"data"`
		}
		if err := json.Unmarshal(page.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if page.Code != http.StatusOK || !body.Data.OAuthEnabled || body.Data.Flash.Message != message {
			t.Fatal("login page lost its retry control or translated unavailable message")
		}
	}
	if idp.discoveryRequests.Load() != 1 {
		t.Fatal("rendering the login page retried discovery")
	}
	idp.discoveryStatus.Store(0)
	if status, _ := anonymous.do(http.MethodGet, "/auth/login", nil); status != http.StatusFound {
		t.Fatal("login did not recover after the provider became available")
	}
	state, nonce := startState(t, anonymous)
	idp.lastNonce = nonce
	if status, _ := anonymous.do(http.MethodGet, "/oauth/callback?code=good&state="+url.QueryEscape(state), nil); status != http.StatusFound {
		t.Fatal("recovered provider could not complete login")
	}
	if status, _ := anonymous.do(http.MethodGet, "/api/v1/me", nil); status != http.StatusOK {
		t.Fatal("recovered login did not create a session")
	}
	if idp.discoveryRequests.Load() != 2 {
		t.Fatal("login retry did not cache successful discovery")
	}
}

func TestUnconfiguredOAuthRemainsDisabled(t *testing.T) {
	_, router, _ := testServer(t, false)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auth/login", http.NoBody))
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/login?error=oauth_disabled" {
		t.Fatal("unconfigured OAuth was treated as a temporary outage")
	}
}

func TestServerRejectsCanceledStartup(t *testing.T) {
	srv, _, _ := testServer(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newServer(ctx, srv.cfg, srv.store, srv.log); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled startup, got %v", err)
	}
}
