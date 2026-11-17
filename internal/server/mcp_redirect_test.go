package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestMCPOAuthRegisteredRedirects(t *testing.T) {
	s, router, _ := testServer(t, true)
	redirects := []string{
		"cursor://anysphere.cursor-mcp/oauth/callback",
		"https://www.cursor.com/agents/mcp/oauth/callback",
		"http://localhost:8787/callback",
		"http://remote.example/callback",
		"other-app://callback/oauth?window=main#complete",
		"com.example.app:oauth/callback",
		"../callback",
		"//client.example/callback",
	}
	registered := oauthJSON(t, router, "/oauth/register", map[string]any{
		"client_name": "MCP client", "redirect_uris": redirects,
		"grant_types":    []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	}, http.StatusCreated)
	clientID := registered["client_id"].(string)
	client, err := s.store.MCPClientByID(t.Context(), clientID)
	if err != nil || !slices.Equal(client.RedirectURIs, redirects) || len(registered["redirect_uris"].([]any)) != len(redirects) {
		t.Fatal("registration did not preserve every callback")
	}
	f := mcpUserSession(t, s, router, 1)
	for _, callback := range redirects {
		t.Run(callback, func(t *testing.T) {
			query := mcpAuthorizeQuery(s, clientID)
			query.Set("redirect_uri", callback)
			req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), http.NoBody)
			req.AddCookie(f.cookies["cc_session"])
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/oauth/consent?") {
				t.Fatal("registered callback did not reach consent")
			}
			consent, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			status, result := f.do(http.MethodPost, "/oauth/consent", map[string]any{"request": consent.Query().Get("request"), "approve": true})
			if status != http.StatusOK {
				t.Fatal("consent failed")
			}
			target, err := url.Parse(result["redirect"].(string))
			if err != nil {
				t.Fatal(err)
			}
			response := target.Query()
			original, err := url.Parse(callback)
			if err != nil {
				t.Fatal(err)
			}
			target.RawQuery = original.RawQuery
			if target.String() != callback || response.Get("state") != "client-state" || response.Get("iss") != s.cfg.PublicURL {
				t.Fatal("consent changed the registered callback or response binding")
			}
			for key, value := range original.Query() {
				if !slices.Equal(response[key], value) {
					t.Fatal("consent lost the callback query")
				}
			}
			form := mcpCodeForm(s, clientID, response.Get("code"))
			form.Set("redirect_uri", "cursor://another-app/oauth/callback")
			oauthPost(t, router, "/oauth/token", form, http.StatusBadRequest)
			form.Set("redirect_uri", callback)
			tokens := oauthPost(t, router, "/oauth/token", form, http.StatusOK)
			mcpCall(t, router, tokens["access_token"].(string), "get_identity", nil, http.StatusOK)
		})
	}
}

func TestMCPOAuthRegistrationPreservesRedirectValues(t *testing.T) {
	s, router, _ := testServer(t, true)
	for _, callback := range []string{
		"cursor://another-app/oauth/callback",
		"cursor://anysphere.cursor-mcp/other-command",
		"cursor://anysphere.cursor-mcp:1234/oauth/callback",
		"cursor://user@anysphere.cursor-mcp/oauth/callback",
		"cursor://anysphere.cursor-mcp/oauth/callback#fragment",
		"cursor://anysphere.cursor-mcp/oauth/callback?command=other",
		"cursor://anysphere.cursor-mcp/%6fauth/callback",
		"javascript://anysphere.cursor-mcp/oauth/callback",
		"file://anysphere.cursor-mcp/oauth/callback",
		"data://anysphere.cursor-mcp/oauth/callback",
		"http://remote.example/callback",
		"https://*.example/callback",
		"not a URI",
		"https://[invalid",
		"",
		"https://client.example/" + strings.Repeat("a", 2049),
	} {
		t.Run(callback, func(t *testing.T) {
			registered := oauthJSON(t, router, "/oauth/register", map[string]any{"redirect_uris": []string{callback}}, http.StatusCreated)
			client, err := s.store.MCPClientByID(t.Context(), registered["client_id"].(string))
			if err != nil || !slices.Equal(client.RedirectURIs, []string{callback}) || registered["redirect_uris"].([]any)[0] != callback {
				t.Fatal("registration changed the callback value")
			}
		})
	}
}
