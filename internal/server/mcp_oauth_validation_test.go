package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/internal/store"
)

func TestMCPOAuthDiscovery(t *testing.T) {
	s, router, _ := testServer(t, true)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", http.NoBody))
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `resource_metadata="`+s.cfg.PublicURL+`/.well-known/oauth-protected-resource/mcp"`) {
			t.Fatal("MCP did not advertise OAuth discovery")
		}
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		metadata := oauthResponse(t, rec, http.StatusOK)
		if strings.Contains(path, "protected-resource") {
			if metadata["resource"] != s.cfg.PublicURL+"/mcp" || metadata["authorization_servers"].([]any)[0] != s.cfg.PublicURL {
				t.Fatal("wrong protected resource metadata")
			}
		} else if metadata["issuer"] != s.cfg.PublicURL || metadata["code_challenge_methods_supported"].([]any)[0] != "S256" {
			t.Fatal("wrong authorization server metadata")
		}
	}
	_, disabled, _ := testServer(t, false)
	rec := httptest.NewRecorder()
	disabled.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", http.NoBody))
	oauthResponse(t, rec, http.StatusServiceUnavailable)
}

func TestMCPOAuthRejectsInvalidClientsAndAuthorization(t *testing.T) {
	s, router, _ := testServer(t, true)
	for _, redirects := range [][]string{nil, {}, make([]string, 11)} {
		oauthJSON(t, router, "/oauth/register", map[string]any{"redirect_uris": redirects}, http.StatusBadRequest)
	}
	for _, metadata := range []map[string]any{
		{"token_endpoint_auth_method": "client_secret_basic"},
		{"grant_types": []string{"password"}},
		{"response_types": []string{"token"}},
		{"scope": "admin"},
	} {
		metadata["redirect_uris"] = []string{mcpTestRedirect}
		oauthJSON(t, router, "/oauth/register", metadata, http.StatusBadRequest)
	}
	clientID := registerMCPClient(t, router)
	for _, tc := range []struct{ field, value, code string }{
		{"redirect_uri", "https://attacker.example/callback", ""},
		{"client_id", "unknown", ""},
		{"response_type", "token", "unsupported_response_type"},
		{"resource", "https://another.example/mcp", "invalid_target"},
		{"resource", "", "invalid_target"},
		{"scope", "admin", "invalid_scope"},
		{"code_challenge_method", "plain", "invalid_request"},
		{"code_challenge", "invalid", "invalid_request"},
	} {
		query := mcpAuthorizeQuery(s, clientID)
		query.Set(tc.field, tc.value)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), http.NoBody))
		if tc.code == "" {
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("untrusted %s caused redirect", tc.field)
			}
			continue
		}
		redirect, err := url.Parse(rec.Header().Get("Location"))
		if err != nil || rec.Code != http.StatusFound || redirect.Query().Get("error") != tc.code || redirect.Query().Get("state") != "client-state" {
			t.Fatalf("invalid %s did not return %s", tc.field, tc.code)
		}
	}
	query := mcpAuthorizeQuery(s, clientID)
	query.Add("resource", "https://attacker.example")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), http.NoBody))
	oauthResponse(t, rec, http.StatusBadRequest)
}

func TestMCPOAuthCodeBinding(t *testing.T) {
	s, router, _ := testServer(t, true)
	f := mcpUserSession(t, s, router, 1)
	clientID := registerMCPClient(t, router)
	otherClient := registerMCPClient(t, router)
	code := mcpConsentCode(t, s, f, clientID, true)
	for _, tc := range []struct{ field, value string }{
		{"client_id", otherClient},
		{"redirect_uri", "https://attacker.example/callback"},
		{"resource", "https://other.example/mcp"},
		{"code_verifier", strings.Repeat("w", 43)},
		{"code_verifier", ""},
		{"code_verifier", strings.Repeat("v", 42)},
		{"code_verifier", strings.Repeat("v", 42) + "!"},
	} {
		form := mcpCodeForm(s, clientID, code)
		form.Set(tc.field, tc.value)
		oauthPost(t, router, "/oauth/token", form, http.StatusBadRequest)
	}
	duplicate := mcpCodeForm(s, clientID, code)
	duplicate.Add("client_id", otherClient)
	oauthPost(t, router, "/oauth/token", duplicate, http.StatusBadRequest)
	tokens := oauthPost(t, router, "/oauth/token", mcpCodeForm(s, clientID, code), http.StatusOK)
	access := tokens["access_token"].(string)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {otherClient}, "resource": {s.cfg.PublicURL + "/mcp"}, "refresh_token": {tokens["refresh_token"].(string)}}
	oauthPost(t, router, "/oauth/token", refresh, http.StatusBadRequest)
	oauthPost(t, router, "/oauth/revoke", url.Values{"client_id": {otherClient}, "token": {access}}, http.StatusOK)
	mcpCall(t, router, access, "get_identity", nil, http.StatusOK)
	for _, header := range []string{"ApiKey " + access, "Bearer invalid", "Bearer " + tokens["refresh_token"].(string), "Bearer access"} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
		req.AddCookie(f.cookies["cc_session"])
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("invalid credentials fell back to a session")
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp?access_token="+url.QueryEscape(access), http.NoBody))
	if rec.Code != http.StatusUnauthorized {
		t.Fatal("token accepted from query string")
	}
}

func TestMCPOAuthConsentBoundToUser(t *testing.T) {
	s, router, _ := testServer(t, true)
	clientID := registerMCPClient(t, router)
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+mcpAuthorizeQuery(s, clientID).Encode(), http.NoBody)
	owner := mcpUserSession(t, s, router, 1)
	req.AddCookie(owner.cookies["cc_session"])
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	consentURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	anonymous := oauthJSON(t, router, "/oauth/consent", map[string]any{"request": consentURL.Query().Get("request"), "approve": true}, http.StatusUnauthorized)
	if anonymous["error"] != "login_required" {
		t.Fatal("anonymous consent was not rejected")
	}
	other, err := s.store.CreateUser(t.Context(), store.User{Email: "other@example.com", Name: "Other", Role: store.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	f := mcpUserSession(t, s, router, other.ID)
	if status, _ := f.do(http.MethodPost, "/oauth/consent", map[string]any{"request": consentURL.Query().Get("request"), "approve": true}); status != http.StatusBadRequest {
		t.Fatal("another user approved the authorization")
	}
}
