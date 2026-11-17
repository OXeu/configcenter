package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OXeu/configcenter/internal/store"
)

func mcpCall(t *testing.T, router http.Handler, token, name string, args map[string]any, want int) map[string]any {
	t.Helper()
	rec := keyRequest(t, router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, http.StatusOK)
	var reply struct {
		Error  *rpcError `json:"error"`
		Result struct {
			IsError    bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error != nil {
		t.Fatalf("%s: RPC error %d: %s", name, reply.Error.Code, reply.Error.Message)
	}
	if reply.Result.Structured["http_status"] != float64(want) || reply.Result.IsError != (want >= 400) {
		t.Fatalf("%s: HTTP status %v, want %d", name, reply.Result.Structured["http_status"], want)
	}
	if len(reply.Result.Content) != 1 || reply.Result.Content[0].Type != "text" || !json.Valid([]byte(reply.Result.Content[0].Text)) {
		t.Fatalf("%s: missing JSON text result", name)
	}
	return reply.Result.Structured
}

func TestMCPCompleteAPI(t *testing.T) {
	s, router, _ := testServer(t, false)
	admin, err := s.store.UserByEmail(t.Context(), "admin@localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateSession(t.Context(), store.Session{ID: "admin-mcp", UserID: admin.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f := &flows{t: t, router: router, csrf: "csrf", cookies: map[string]*http.Cookie{"cc_session": {Name: "cc_session", Value: "admin-mcp"}}}
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("operator", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
	seen := map[string]bool{}
	call := func(name string, args map[string]any, want int) map[string]any {
		t.Helper()
		seen[name] = true
		return mcpCall(t, router, token, name, args, want)
	}
	call("get_identity", nil, http.StatusOK)
	call("get_key_scopes", nil, http.StatusOK)
	call("list_projects", nil, http.StatusOK)
	call("create_project", map[string]any{"slug": "billing", "name": "Billing"}, http.StatusCreated)
	call("update_project", map[string]any{"project": "billing", "name": "Billing updated", "description": "managed"}, http.StatusOK)
	call("create_environment", map[string]any{"project": "billing", "slug": "staging", "name": "Staging"}, http.StatusCreated)
	call("rename_environment", map[string]any{"project": "billing", "environment": "staging", "slug": "staging", "name": "Preview"}, http.StatusOK)
	call("set_primary_environment", map[string]any{"project": "billing", "environment": "production"}, http.StatusOK)
	call("list_environments", map[string]any{"project": "billing"}, http.StatusOK)
	call("get_project", map[string]any{"project": "billing"}, http.StatusOK)
	config := map[string]any{"project": "billing", "environment": "staging", "key": "db/pool", "value": ""}
	call("set_config", config, http.StatusOK)
	call("set_configs", map[string]any{"project": "billing", "environment": "staging", "items": []map[string]string{{"key": "db/pool", "value": ""}, {"key": "created.default", "value": "initialized"}}}, http.StatusNoContent)
	selected := call("list_config", map[string]any{"project": "billing", "environment": "staging", "keys": []string{"created.default", "missing"}}, http.StatusOK)
	if len(selected["items"].([]any)) != 1 || len(selected["missing_keys"].([]any)) != 1 {
		t.Fatal("batch MCP read failed", selected)
	}
	delete(config, "value")
	if got := call("get_config", config, http.StatusOK); got["value"] != "" || got["key"] != "db/pool" {
		t.Fatal("encoded key or empty value changed")
	}
	call("list_config", map[string]any{"project": "billing", "environment": "staging", "prefix": "db/"}, http.StatusOK)
	call("list_values", map[string]any{"project": "billing", "environment": "staging"}, http.StatusOK)
	config["value"], config["new_key"] = "20", "db.pool"
	config["new_key"] = "invalid key"
	call("set_config", config, http.StatusBadRequest)
	config["new_key"] = "db.pool"
	call("set_config", config, http.StatusOK)
	delete(config, "value")
	delete(config, "new_key")
	call("get_config", config, http.StatusNotFound)
	config["key"] = "db.pool"
	proposal := call("propose_config", map[string]any{"project": "billing", "environment": "staging", "key": "db.pool", "action": "set", "value": "5", "reason": "tune"}, http.StatusCreated)
	if proposal["status"] != store.StatusPending {
		t.Fatal("proposal status was lost")
	}
	call("list_proposals", map[string]any{"project": "billing", "status": "pending"}, http.StatusOK)
	call("review_proposal", map[string]any{"project": "billing", "id": proposal["id"], "approve": true}, http.StatusOK)
	if got := call("get_config", config, http.StatusOK); got["value"] != "5" {
		t.Fatal("proposal was not applied")
	}
	proposal = call("propose_config", map[string]any{"project": "billing", "environment": "staging", "key": "db.pool", "action": "delete"}, http.StatusCreated)
	call("review_proposal", map[string]any{"project": "billing", "id": proposal["id"], "approve": false}, http.StatusOK)
	call("get_config", config, http.StatusOK)
	child := call("create_key", scopeBody("child", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.ConfigRead}}), http.StatusCreated)
	childToken, _ := child["token"].(string)
	childKey, _ := child["key"].(map[string]any)
	call("list_keys", map[string]any{"project": "billing"}, http.StatusOK)
	update := scopeBody("disabled")
	update["id"] = childKey["id"]
	call("update_key", update, http.StatusOK)
	mcpCall(t, router, childToken, "get_config", config, http.StatusForbidden)
	rotated := call("rotate_key", map[string]any{"id": childKey["id"]}, http.StatusOK)
	newToken, _ := rotated["token"].(string)
	if newToken == "" || newToken == childToken {
		t.Fatal("rotation did not replace token")
	}
	keyRequest(t, router, childToken, http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	call("delete_key", map[string]any{"id": childKey["id"]}, http.StatusNoContent)
	keyRequest(t, router, newToken, http.MethodPost, "/mcp", nil, http.StatusUnauthorized)
	user, err := s.store.CreateUser(t.Context(), store.User{Email: "managed@example.com", Name: "Managed", Role: store.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	call("list_users", nil, http.StatusOK)
	call("update_user_role", map[string]any{"id": user.ID, "role": "admin"}, http.StatusOK)
	updated, err := s.store.UserByID(t.Context(), user.ID)
	if err != nil || updated.Role != store.RoleAdmin {
		t.Fatal("role did not change")
	}
	call("update_user_role", map[string]any{"id": admin.ID, "role": "user"}, http.StatusBadRequest)
	call("delete_user", map[string]any{"id": admin.ID}, http.StatusBadRequest)
	call("delete_user", map[string]any{"id": user.ID}, http.StatusNoContent)
	call("get_settings", nil, http.StatusOK)
	call("update_settings", map[string]any{"allow_registration": false}, http.StatusNoContent)
	allowed, err := s.store.RegistrationAllowed(t.Context())
	if err != nil || allowed {
		t.Fatal("false setting was not saved")
	}
	call("delete_config", config, http.StatusNoContent)
	call("delete_environment", map[string]any{"project": "billing", "environment": "staging"}, http.StatusNoContent)
	call("delete_project", map[string]any{"project": "billing"}, http.StatusNoContent)
	for _, op := range s.apiOperations() {
		if !op.RESTOnly && !seen[op.Name] {
			t.Errorf("API operation %s was not exercised over MCP", op.Name)
		}
	}
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/") && !slices.ContainsFunc(s.apiOperations(), func(op apiOperation) bool { return "/api/v1"+op.Path == route.Path && op.Method == route.Method }) {
			t.Errorf("API route %s %s has no MCP operation", route.Method, route.Path)
		}
	}
}

func TestMCPDiscoveryAndScopeEnforcement(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "alpha")
	createKeyProject(t, f, "beta")
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("limited",
		keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead, store.KeysWrite, store.ProjectsRead}},
		keyScopeJSON{Project: "beta", Permissions: []store.Permission{store.EnvironmentsRead}},
	))
	for _, method := range []string{"initialize", "ping", "tools/list", "resources/list", "prompts/list"} {
		rec := keyRequest(t, f.router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": "request", "method": method}, http.StatusOK)
		var reply struct {
			ID     string    `json:"id"`
			Error  *rpcError `json:"error"`
			Result struct {
				Protocol string    `json:"protocolVersion"`
				Tools    []mcpTool `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != "request" || reply.Error != nil {
			t.Fatalf("%s failed", method)
		}
		if method == "initialize" && reply.Result.Protocol != mcpProtocol {
			t.Fatal("wrong protocol")
		}
		if method == "tools/list" {
			for _, tool := range reply.Result.Tools {
				if slices.Contains([]string{"get_project", "create_project", "set_config", "list_users", "get_settings"}, tool.Name) {
					t.Errorf("discovered unauthorized tool %s", tool.Name)
				}
			}
			if !slices.ContainsFunc(reply.Result.Tools, func(tool mcpTool) bool { return tool.Name == "create_key" }) {
				t.Fatal("missing delegated key management")
			}
		}
	}
	mcpCall(t, f.router, token, "create_project", map[string]any{"slug": "denied", "name": "Denied"}, http.StatusForbidden)
	mcpCall(t, f.router, token, "list_users", nil, http.StatusForbidden)
	mcpCall(t, f.router, token, "get_settings", nil, http.StatusForbidden)
	mcpCall(t, f.router, token, "get_project", map[string]any{"project": "alpha"}, http.StatusForbidden)
	mcpCall(t, f.router, token, "get_project", map[string]any{"project": "beta"}, http.StatusForbidden)
	mcpCall(t, f.router, token, "create_key", scopeBody("child", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.ConfigRead}}), http.StatusCreated)
	for _, scope := range []keyScopeJSON{
		{Project: "alpha", Permissions: []store.Permission{store.ConfigWrite}},
		{Project: "beta", Permissions: []store.Permission{store.ConfigRead}},
		{Project: "*", Permissions: []store.Permission{store.UsersRead}},
	} {
		mcpCall(t, f.router, token, "create_key", scopeBody("escalate", scope), http.StatusForbidden)
	}
	mcpCall(t, f.router, token, "create_key", scopeBody("invalid-global", keyScopeJSON{Project: "alpha", Permissions: []store.Permission{store.UsersRead}}), http.StatusBadRequest)
	global, _ := issueKey(t, f, "/api/v1/keys", scopeBody("settings-reader", keyScopeJSON{Project: "*", Permissions: []store.Permission{store.SettingsRead}}))
	options := mcpCall(t, f.router, global, "get_key_scopes", nil, http.StatusOK)
	projects, ok := options["projects"].([]any)
	if !ok || len(projects) != 0 {
		t.Fatal("scope discovery leaked project metadata without projects:read")
	}
	mcpCall(t, f.router, global, "get_settings", nil, http.StatusOK)
	mcpCall(t, f.router, global, "update_settings", map[string]any{"allow_registration": true}, http.StatusForbidden)
	mcpCall(t, f.router, global, "list_users", nil, http.StatusForbidden)
}

func TestMCPValidationAndAuthentication(t *testing.T) {
	f := newFlows(t)
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("operator", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"unknown_tool", nil},
		{"get_config", map[string]any{"environment": "production", "key": "x"}},
		{"set_config", map[string]any{"project": "configcenter", "environment": "production", "key": "x"}},
		{"delete_key", map[string]any{"id": "1"}},
		{"delete_key", map[string]any{"id": 0}},
		{"delete_key", map[string]any{"id": 1.5}},
		{"update_settings", map[string]any{"allow_registration": "false"}},
		{"list_projects", map[string]any{"url": "https://example.com"}},
		{"create_key", map[string]any{"name": "invalid", "scopes": []any{map[string]any{"project": "*", "permissions": []any{"unknown"}}}}},
	} {
		rec := keyRequest(t, f.router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tc.name, "arguments": tc.args}}, http.StatusOK)
		var reply struct {
			Error *rpcError `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || reply.Error == nil || reply.Error.Code != codeInvalidParams {
			t.Fatalf("%s accepted invalid arguments", tc.name)
		}
	}
	mcpCall(t, f.router, token, "propose_config", map[string]any{"project": "configcenter", "environment": "production", "key": "x", "action": "set"}, http.StatusBadRequest)
	notification := keyRequest(t, f.router, token, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, http.StatusAccepted)
	if notification.Body.Len() != 0 {
		t.Fatal("notification returned a body")
	}
	for _, payload := range []string{`{`, `{} {}`, `[]`} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatal("malformed request accepted")
		}
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "update_settings", "arguments": map[string]any{"allow_registration": false}}}
	status, reply := f.do(http.MethodPost, "/mcp", body)
	result, _ := reply["result"].(map[string]any)
	if status != http.StatusOK || result["isError"] != false {
		t.Fatal("authorized session could not write")
	}
	f.csrf = ""
	if status, _ := f.do(http.MethodPost, "/mcp", body); status != http.StatusForbidden {
		t.Fatal("session bypassed CSRF")
	}
	for _, credential := range []string{token, "cc_invalid"} {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(encoded)))
		req.AddCookie(f.cookies["cc_session"])
		req.Header.Set("Authorization", "Bearer "+credential)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		want := http.StatusOK
		if credential != token {
			want = http.StatusUnauthorized
		}
		if rec.Code != want {
			t.Fatal("explicit API key did not take precedence over session")
		}
	}
	for _, path := range []string{"/mcp/configcenter", "/api/v1/projects/configcenter/keys", "/api/v1/ui/configcenter/items/production"} {
		keyRequest(t, f.router, token, http.MethodPost, path, nil, http.StatusNotFound)
	}
}

func TestMCPTransport(t *testing.T) {
	f := newFlows(t)
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("client"))
	for _, tc := range []struct {
		method, origin, version string
		want                    int
	}{
		{http.MethodPost, "http://cc.test", mcpProtocol, http.StatusOK},
		{http.MethodPost, "https://untrusted.example", mcpProtocol, http.StatusForbidden},
		{http.MethodPost, "null", mcpProtocol, http.StatusForbidden},
		{http.MethodPost, "", "unsupported", http.StatusBadRequest},
		{http.MethodGet, "http://cc.test", "", http.StatusMethodNotAllowed},
		{http.MethodDelete, "http://cc.test", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "https://untrusted.example", "", http.StatusForbidden},
	} {
		req := httptest.NewRequest(tc.method, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("MCP-Protocol-Version", tc.version)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: %d, want %d", tc.method, tc.origin, rec.Code, tc.want)
		}
	}
}

func TestMCPSessionOwnership(t *testing.T) {
	s, router, _ := testServer(t, false)
	user, err := s.store.CreateUser(t.Context(), store.User{Email: "owner@example.com", Name: "Owner", Role: store.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateSession(t.Context(), store.Session{ID: "owner-session", UserID: user.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f := &flows{t: t, router: router, csrf: "csrf", cookies: map[string]*http.Cookie{"cc_session": {Name: "cc_session", Value: "owner-session"}}}
	createKeyProject(t, f, "owned")
	for _, tc := range []struct {
		name      string
		args      map[string]any
		forbidden bool
	}{
		{"set_config", map[string]any{"project": "owned", "environment": "production", "key": "x", "value": "1"}, false},
		{"set_config", map[string]any{"project": "configcenter", "environment": "production", "key": "x", "value": "1"}, true},
		{"list_users", nil, true},
		{"get_settings", nil, true},
		{"create_key", scopeBody("forbidden", keyScopeJSON{Project: "*", Permissions: []store.Permission{store.SettingsWrite}}), true},
	} {
		status, reply := f.do(http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tc.name, "arguments": tc.args}})
		result, _ := reply["result"].(map[string]any)
		if status != http.StatusOK || result["isError"] != tc.forbidden {
			t.Fatalf("session authorization failed for %s", tc.name)
		}
	}
}
