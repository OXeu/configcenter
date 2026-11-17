package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/OXeu/configcenter/internal/store"
)

const mcpTestRedirect = "http://127.0.0.1:4567/callback?existing=value"

func oauthJSON(t *testing.T, router http.Handler, path string, body any, want int) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return oauthResponse(t, rec, want)
}

func oauthResponse(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("OAuth status %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
	var result map[string]any
	if rec.Body.Len() != 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func registerMCPClient(t *testing.T, router http.Handler) string {
	t.Helper()
	result := oauthJSON(t, router, "/oauth/register", map[string]any{
		"client_name": "Test client", "redirect_uris": []string{mcpTestRedirect},
		"grant_types": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_method": "none",
	}, http.StatusCreated)
	return result["client_id"].(string)
}

func mcpAuthorizeQuery(s *Server, clientID string) url.Values {
	challenge := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	return url.Values{
		"client_id": {clientID}, "redirect_uri": {mcpTestRedirect}, "response_type": {"code"},
		"resource": {s.cfg.PublicURL + "/mcp"}, "scope": {"mcp"}, "state": {"client-state"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}
}

func oauthPost(t *testing.T, router http.Handler, path string, form url.Values, want int) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("OAuth credentials may be cached")
	}
	return oauthResponse(t, rec, want)
}

func mcpCodeForm(s *Server, clientID, code string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {mcpTestRedirect}, "resource": {s.cfg.PublicURL + "/mcp"}, "code_verifier": {strings.Repeat("v", 43)},
	}
}

func mcpConsentCode(t *testing.T, s *Server, f *flows, clientID string, approve bool) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+mcpAuthorizeQuery(s, clientID).Encode(), http.NoBody)
	req.AddCookie(f.cookies["cc_session"])
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/oauth/consent?") {
		t.Fatalf("missing consent page: %d %s", rec.Code, rec.Body.String())
	}
	consentURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	status, result := f.do(http.MethodPost, "/oauth/consent", map[string]any{"request": consentURL.Query().Get("request"), "approve": approve})
	if status != http.StatusOK {
		t.Fatalf("consent failed: %d %v", status, result)
	}
	redirect, err := url.Parse(result["redirect"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Query().Get("state") != "client-state" || redirect.Query().Get("iss") != s.cfg.PublicURL || redirect.Query().Get("existing") != "value" {
		t.Fatal("authorization response lost state, issuer or callback query")
	}
	if !approve && redirect.Query().Get("error") != "access_denied" {
		t.Fatal("denial was not returned to client")
	}
	return redirect.Query().Get("code")
}

func mcpUserSession(t *testing.T, s *Server, router http.Handler, userID uint) *flows {
	t.Helper()
	id := "mcp-session-" + rand.Text()
	if err := s.store.CreateSession(t.Context(), store.Session{ID: id, UserID: userID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return &flows{t: t, router: router, csrf: "csrf", cookies: map[string]*http.Cookie{"cc_session": {Name: "cc_session", Value: id}}}
}

func TestMCPOAuthLoginAndConsent(t *testing.T) {
	s, router, idp := testServer(t, true)
	clientID := registerMCPClient(t, router)
	cookies := &jar{cookies: map[string]*http.Cookie{}}
	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+mcpAuthorizeQuery(s, clientID).Encode(), http.NoBody))
	if start.Code != http.StatusFound {
		t.Fatal("authorization did not start login")
	}
	cookies.add(start.Result())
	upstream, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	idp.lastNonce = upstream.Query().Get("nonce")
	callback := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=good&state="+url.QueryEscape(upstream.Query().Get("state")), http.NoBody)
	callback.Header.Set("Cookie", cookies.header())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, callback)
	cookies.add(rec.Result())
	consentURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || consentURL.Path != "/oauth/consent" {
		t.Fatalf("login lost authorization request: %s", rec.Header().Get("Location"))
	}
	pageRequest := httptest.NewRequest(http.MethodGet, consentURL.String(), http.NoBody)
	pageRequest.Header.Set("Cookie", cookies.header())
	page := httptest.NewRecorder()
	router.ServeHTTP(page, pageRequest)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"page":"oauth-consent"`) || !strings.Contains(page.Body.String(), "Test client") {
		t.Fatal("consent page not rendered")
	}
	session, _, err := s.store.SessionUser(t.Context(), cookies.cookies["cc_session"].Value)
	if err != nil {
		t.Fatal(err)
	}
	f := &flows{t: t, router: router, csrf: "", cookies: cookies.cookies}
	body := map[string]any{"request": consentURL.Query().Get("request"), "approve": true}
	if status, _ := f.do(http.MethodPost, "/oauth/consent", body); status != http.StatusForbidden {
		t.Fatal("consent bypassed CSRF")
	}
	f.csrf = session.CSRFToken
	status, result := f.do(http.MethodPost, "/oauth/consent", body)
	if status != http.StatusOK {
		t.Fatalf("consent failed: %v", result)
	}
	if status, _ := f.do(http.MethodPost, "/oauth/consent", body); status != http.StatusBadRequest {
		t.Fatal("consent replay was accepted")
	}
	redirect, err := url.Parse(result["redirect"].(string))
	if err != nil {
		t.Fatal(err)
	}
	form := mcpCodeForm(s, clientID, redirect.Query().Get("code"))
	tokens := oauthPost(t, router, "/oauth/token", form, http.StatusOK)
	if tokens["token_type"] != "Bearer" || tokens["expires_in"] != float64(3600) {
		t.Fatal("invalid token response")
	}
	access := tokens["access_token"].(string)
	identity := mcpCall(t, router, access, "get_identity", nil, http.StatusOK)
	if identity["email"] != "dana@example.com" || identity["csrf"] != "" {
		t.Fatal("OAuth identity did not match login or leaked session CSRF")
	}
	keyRequest(t, router, access, http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
	oauthPost(t, router, "/oauth/token", form, http.StatusBadRequest)
	keyRequest(t, router, access, http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
}

func TestMCPOAuthPermissionsRefreshAndRevocation(t *testing.T) {
	s, router, _ := testServer(t, true)
	user, err := s.store.CreateUser(t.Context(), store.User{Email: "owner@example.com", Name: "Owner", Role: store.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	f := mcpUserSession(t, s, router, user.ID)
	createKeyProject(t, f, "owned")
	clientID := registerMCPClient(t, router)
	if code := mcpConsentCode(t, s, f, clientID, false); code != "" {
		t.Fatal("denial issued a code")
	}
	issue := func() map[string]any {
		t.Helper()
		code := mcpConsentCode(t, s, f, clientID, true)
		return oauthPost(t, router, "/oauth/token", mcpCodeForm(s, clientID, code), http.StatusOK)
	}
	tokens := issue()
	access := tokens["access_token"].(string)
	mcpCall(t, router, access, "set_config", map[string]any{"project": "owned", "environment": "production", "key": "x", "value": "1"}, http.StatusOK)
	mcpCall(t, router, access, "list_values", map[string]any{"project": "owned", "environment": "production"}, http.StatusOK)
	mcpCall(t, router, access, "get_project", map[string]any{"project": "owned"}, http.StatusOK)
	mcpCall(t, router, access, "get_project", map[string]any{"project": "configcenter"}, http.StatusForbidden)
	mcpCall(t, router, access, "list_config", map[string]any{"project": "configcenter", "environment": "production"}, http.StatusForbidden)
	mcpCall(t, router, access, "list_users", nil, http.StatusForbidden)
	if err := s.store.SetUserRole(t.Context(), user.ID, store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	mcpCall(t, router, access, "list_users", nil, http.StatusOK)
	if err := s.store.SetUserRole(t.Context(), user.ID, store.RoleUser); err != nil {
		t.Fatal(err)
	}
	mcpCall(t, router, access, "list_users", nil, http.StatusForbidden)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {tokens["refresh_token"].(string)}, "resource": {s.cfg.PublicURL + "/mcp"}}
	next := oauthPost(t, router, "/oauth/token", refresh, http.StatusOK)
	keyRequest(t, router, access, http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	mcpCall(t, router, next["access_token"].(string), "get_identity", nil, http.StatusOK)
	oauthPost(t, router, "/oauth/token", refresh, http.StatusBadRequest)
	keyRequest(t, router, next["access_token"].(string), http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	tokens = issue()
	oauthPost(t, router, "/oauth/revoke", url.Values{"client_id": {clientID}, "token": {tokens["access_token"].(string)}}, http.StatusOK)
	keyRequest(t, router, tokens["access_token"].(string), http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	tokens = issue()
	if err := s.store.DeleteUser(t.Context(), user.ID, 1); err != nil {
		t.Fatal(err)
	}
	keyRequest(t, router, tokens["access_token"].(string), http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	refresh.Set("refresh_token", tokens["refresh_token"].(string))
	oauthPost(t, router, "/oauth/token", refresh, http.StatusBadRequest)
}
