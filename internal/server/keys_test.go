package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OXeu/configcenter/internal/store"
)

func keyRequest(t *testing.T, router http.Handler, token, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s: status %d, want %d", method, path, rec.Code, want)
	}
	return rec
}

func issueKey(t *testing.T, f *flows, path string, body map[string]any) (token string, keyID uint) {
	t.Helper()
	status, response := f.do(http.MethodPost, path, body)
	if status != http.StatusCreated {
		t.Fatalf("issue key: status %d", status)
	}
	token, ok := response["token"].(string)
	if !ok || !strings.HasPrefix(token, "cc_") {
		t.Fatal("missing issued credential")
	}
	key, ok := response["key"].(map[string]any)
	if !ok {
		t.Fatal("missing key metadata")
	}
	id, ok := key["id"].(float64)
	if !ok {
		t.Fatal("missing key ID")
	}
	return token, uint(id)
}

func scopeBody(name string, scopes ...keyScopeJSON) map[string]any {
	if scopes == nil {
		scopes = []keyScopeJSON{}
	}
	return map[string]any{"name": name, "scopes": scopes}
}

func createKeyProject(t *testing.T, f *flows, slug string) {
	t.Helper()
	status, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]any{"slug": slug, "name": slug})
	if status != http.StatusCreated {
		t.Fatalf("create project: %d", status)
	}
}

func TestScopedKeyLifecycleAndManagement(t *testing.T) {
	f := newFlows(t)
	token, id := issueKey(t, f, "/api/v1/keys", scopeBody("automation", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
	for _, project := range []string{"billing", "shipping"} {
		base := "/api/v1/projects/" + project
		keyRequest(t, f.router, token, http.MethodPost, "/api/v1/projects", map[string]any{"slug": project, "name": project}, http.StatusCreated)
		keyRequest(t, f.router, token, http.MethodPatch, base, map[string]any{"name": "Updated", "description": "Managed"}, http.StatusOK)
		keyRequest(t, f.router, token, http.MethodPost, base+"/environments", map[string]any{"slug": "staging", "name": "Staging"}, http.StatusCreated)
		keyRequest(t, f.router, token, http.MethodPut, base+"/environments/staging/items/db.pool", map[string]any{"value": "20"}, http.StatusOK)
		rec := keyRequest(t, f.router, token, http.MethodGet, base+"/environments/staging/items", nil, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"value":"20"`) {
			t.Fatal("cannot read written configuration")
		}
		keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/project?project="+project, nil, http.StatusOK)
		keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/environments/staging/values?project="+project, nil, http.StatusOK)
		keyRequest(t, f.router, token, http.MethodDelete, base+"/environments/staging/items/db.pool", nil, http.StatusNoContent)
		keyRequest(t, f.router, token, http.MethodDelete, base+"/environments/staging", nil, http.StatusNoContent)
	}
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/project", nil, http.StatusBadRequest)
	keyRequest(t, f.router, token, http.MethodDelete, "/api/v1/projects/shipping", nil, http.StatusNoContent)
	for _, path := range []string{"/api/v1/users", "/api/v1/settings"} {
		keyRequest(t, f.router, token, http.MethodGet, path, nil, http.StatusOK)
	}
	rec := keyRequest(t, f.router, token, http.MethodGet, "/api/v1/keys", nil, http.StatusOK)
	if strings.Contains(rec.Body.String(), token) || strings.Contains(rec.Body.String(), "digest") || !strings.Contains(rec.Body.String(), `"project":"*"`) {
		t.Fatal("key listing must contain scopes without credentials")
	}
	path := fmt.Sprintf("/api/v1/keys/%d", id)
	status, _ := f.do(http.MethodPatch, path, scopeBody("reader", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.ConfigRead}}))
	if status != http.StatusOK {
		t.Fatalf("edit scopes: %d", status)
	}
	keyRequest(t, f.router, token, http.MethodPut, "/api/v1/projects/billing/environments/production/items/value", map[string]any{"value": "blocked"}, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusOK)
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/keys", nil, http.StatusForbidden)
	status, response := f.do(http.MethodPost, path+"/rotate", nil)
	rotated, ok := response["token"].(string)
	if status != http.StatusOK || !ok || rotated == token {
		t.Fatal("rotation did not return a new credential")
	}
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusUnauthorized)
	keyRequest(t, f.router, rotated, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusOK)
	keyRequest(t, f.router, rotated, http.MethodDelete, "/api/v1/projects/billing/environments/production/items/value", nil, http.StatusForbidden)
	status, _ = f.do(http.MethodDelete, path, nil)
	if status != http.StatusNoContent {
		t.Fatalf("revoke key: %d", status)
	}
	keyRequest(t, f.router, rotated, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusUnauthorized)
}

func TestScopesBindEachPermissionToItsProject(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "alpha")
	createKeyProject(t, f, "beta")
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("mixed",
		keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead}},
		keyScopeJSON{Project: "beta", Permissions: []store.Permission{store.ConfigWrite}},
	))
	for _, tc := range []struct {
		method, project string
		want            int
	}{
		{http.MethodGet, "alpha", http.StatusOK},
		{http.MethodPut, "alpha", http.StatusForbidden},
		{http.MethodGet, "beta", http.StatusForbidden},
		{http.MethodPut, "beta", http.StatusOK},
		{http.MethodDelete, "beta", http.StatusForbidden},
	} {
		path := "/api/v1/projects/" + tc.project + "/environments/production/items"
		if tc.method != http.MethodGet {
			path += "/value"
		}
		keyRequest(t, f.router, token, tc.method, path, map[string]any{"value": "20"}, tc.want)
	}
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/projects/alpha/environments", nil, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusBadRequest)
	keyRequest(t, f.router, token, http.MethodGet, "/api/v1/agent/environments/production/values?project=alpha", nil, http.StatusOK)
	for _, tc := range []struct {
		project string
		tool    string
	}{{"alpha", "set_config"}, {"beta", "get_config"}} {
		rec := keyRequest(t, f.router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, http.StatusOK)
		var listed struct {
			Result struct {
				Tools []mcpTool `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed.Result.Tools) != 7 {
			t.Fatal("MCP discovery did not filter scopes")
		}
		args := map[string]any{"project": tc.project, "environment": "production", "key": "value"}
		if tc.tool == "set_config" {
			args["value"] = "30"
		}
		rec = keyRequest(t, f.router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tc.tool, "arguments": args}}, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"isError":true`) {
			t.Fatal("direct MCP call bypassed scopes")
		}
	}
}

func TestKeyDelegationCannotEscalate(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "alpha")
	createKeyProject(t, f, "beta")
	parent, _ := issueKey(t, f, "/api/v1/keys", scopeBody("parent", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.KeysRead, store.KeysWrite, store.ConfigRead}}))
	_, broadID := issueKey(t, f, "/api/v1/keys", scopeBody("broad", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
	for _, scope := range []keyScopeJSON{
		{Project: "alpha", Permissions: []store.Permission{store.ConfigWrite}},
		{Project: "beta", Permissions: []store.Permission{store.ConfigRead}},
		{Project: "*", Permissions: []store.Permission{store.ConfigRead}},
	} {
		keyRequest(t, f.router, parent, http.MethodPost, "/api/v1/keys", scopeBody("forbidden", scope), http.StatusForbidden)
	}
	keyRequest(t, f.router, parent, http.MethodPost, "/api/v1/keys", scopeBody("child", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead}}), http.StatusCreated)
	broadPath := fmt.Sprintf("/api/v1/keys/%d", broadID)
	keyRequest(t, f.router, parent, http.MethodPatch, broadPath, scopeBody("hijacked"), http.StatusForbidden)
	keyRequest(t, f.router, parent, http.MethodPost, broadPath+"/rotate", nil, http.StatusForbidden)
	keyRequest(t, f.router, parent, http.MethodDelete, broadPath, nil, http.StatusForbidden)
	rec := keyRequest(t, f.router, parent, http.MethodGet, "/api/v1/keys", nil, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"name":"broad"`) {
		t.Fatal("broader key metadata leaked")
	}
}

func TestScopeValidationExpiryAndEmptyGrants(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "alpha")
	for _, body := range []map[string]any{
		{"name": "missing"},
		scopeBody("invalid", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{"config:admin"}}),
		scopeBody("invalid", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{}}),
		scopeBody("invalid", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead, store.ConfigRead}}),
		scopeBody("invalid", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead}}, keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigWrite}}),
	} {
		status, _ := f.do(http.MethodPost, "/api/v1/keys", body)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid scope accepted: %d", status)
		}
	}
	empty, _ := issueKey(t, f, "/api/v1/keys", scopeBody("disabled"))
	keyRequest(t, f.router, empty, http.MethodGet, "/api/v1/projects/alpha/environments/production/items", nil, http.StatusForbidden)
	body := scopeBody("expired", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()})
	body["expires_at"] = time.Now().Add(-time.Hour)
	expired, _ := issueKey(t, f, "/api/v1/keys", body)
	keyRequest(t, f.router, expired, http.MethodGet, "/api/v1/projects", nil, http.StatusUnauthorized)
	saved := f.csrf
	f.csrf = ""
	status, _ := f.do(http.MethodPost, "/api/v1/keys", scopeBody("no-csrf"))
	if status != http.StatusForbidden {
		t.Fatal("key creation bypassed CSRF")
	}
	f.csrf = saved
}
