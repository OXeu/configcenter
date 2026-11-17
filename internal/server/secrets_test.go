package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/internal/store"
)

const testSecret = "sk-confidential-middle-x123"

func assertSecretMasked(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "confidential-middle") {
		t.Fatal("response disclosed secret plaintext")
	}
}

func TestSecretMask(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"", "****"},
		{"a", "****"},
		{"12345678", "****"},
		{testSecret, "sk-****x123"},
		{"密钥测试不可公开末尾校验", "密钥测****末尾校验"},
	} {
		if got := maskSecret(tc.value); got != tc.want {
			t.Fatal("incorrect secret mask")
		}
	}
}

func TestSecretReadScopes(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "secrets")
	createKeyProject(t, f, "other")
	base := "/api/v1/projects/secrets/environments/production/items"
	status, created := f.do(http.MethodPut, base+"/token", map[string]any{"value": testSecret, "secret": true})
	if status != http.StatusOK || created["value"] != "sk-****x123" || created["masked"] != true {
		t.Fatal("secret write did not return a mask")
	}
	assertSecretMasked(t, created)
	reader, _ := issueKey(t, f, "/api/v1/keys", scopeBody("reader", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ConfigRead, store.ConfigWrite, store.KeysWrite}}))
	privileged, _ := issueKey(t, f, "/api/v1/keys", scopeBody("privileged", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ConfigRead, store.ConfigReadSecrets}}))
	execution, _ := issueKey(t, f, "/api/v1/keys", scopeBody("execution", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ConfigReadSecrets}}))
	keyRequest(t, f.router, execution, http.MethodGet, base, nil, http.StatusForbidden)
	keyRequest(t, f.router, execution, http.MethodGet, "/api/v1/agent/environments/production/values/reveal", nil, http.StatusOK)
	for _, token := range []string{reader, privileged} {
		for _, path := range []string{base, base + "/token", base + "?keys=token", base + "?prefix=tok", base + "/token?reveal=true", "/api/v1/agent/environments/production/values", "/api/v1/agent/environments/production/values?keys=token"} {
			rec := keyRequest(t, f.router, token, http.MethodGet, path, nil, http.StatusOK)
			assertSecretMasked(t, rec.Body.String())
			if !strings.Contains(rec.Body.String(), "sk-****x123") {
				t.Fatal("ordinary read did not return the verification mask")
			}
		}
	}
	if status, result := f.do(http.MethodGet, base, nil); status != http.StatusOK {
		t.Fatal(status)
	} else {
		assertSecretMasked(t, result)
	}
	keyRequest(t, f.router, reader, http.MethodPost, base+"/token/reveal", nil, http.StatusForbidden)
	keyRequest(t, f.router, reader, http.MethodGet, "/api/v1/agent/environments/production/values/reveal", nil, http.StatusForbidden)
	keyRequest(t, f.router, reader, http.MethodPost, "/api/v1/keys", scopeBody("escalate", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ConfigReadSecrets}}), http.StatusForbidden)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, base + "/token/reveal"},
		{http.MethodGet, "/api/v1/agent/environments/production/values/reveal?keys=token"},
	} {
		rec := keyRequest(t, f.router, privileged, endpoint.method, endpoint.path, nil, http.StatusOK)
		if !strings.Contains(rec.Body.String(), testSecret) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("privileged read failed or allowed caching")
		}
	}
	keyRequest(t, f.router, privileged, http.MethodPost, "/api/v1/projects/other/environments/production/items/token/reveal", nil, http.StatusForbidden)
	keyRequest(t, f.router, privileged, http.MethodGet, "/api/v1/agent/environments/production/values/reveal?project=other", nil, http.StatusForbidden)
	for _, body := range []map[string]any{{"secret": false}, {"value": "replacement", "secret": false}} {
		keyRequest(t, f.router, reader, http.MethodPut, base+"/token", body, http.StatusForbidden)
	}
	keyRequest(t, f.router, reader, http.MethodPut, base+"/token", map[string]any{"description": "Updated metadata"}, http.StatusOK)
	keyRequest(t, f.router, reader, http.MethodPut, base, map[string]any{"items": []map[string]any{{"key": "token", "value": testSecret}}}, http.StatusNoContent)
	if status, result := f.do(http.MethodPost, base+"/token/reveal", nil); status != http.StatusOK || result["value"] != testSecret {
		t.Fatal("metadata or batch write replaced the stored secret")
	}
	f.csrf = ""
	if status, _ := f.do(http.MethodPost, base+"/token/reveal", nil); status != http.StatusForbidden {
		t.Fatal("browser reveal bypassed CSRF")
	}
}

func TestSecretSystemSetting(t *testing.T) {
	s, router, _ := testServer(t, false)
	admin, err := s.store.UserByEmail(t.Context(), "admin@localhost")
	if err != nil {
		t.Fatal(err)
	}
	f := mcpUserSession(t, s, router, admin.ID)
	path := "/api/v1/projects/configcenter/environments/production/items/allow_registration"
	if status, _ := f.do(http.MethodPut, path, map[string]any{"value": "true", "secret": true}); status != http.StatusOK {
		t.Fatal(status)
	}
	if status, reply := f.do(http.MethodGet, "/api/v1/settings", nil); status != http.StatusOK || reply["settings"].([]any)[0].(map[string]any)["value"] != "****" {
		t.Fatal("settings API disclosed a secret value")
	}
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("settings", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
	reply := mcpCall(t, router, token, "get_settings", nil, http.StatusOK)
	if reply["settings"].([]any)[0].(map[string]any)["value"] != "****" {
		t.Fatal("MCP settings disclosed a secret value")
	}
}

func TestMCPSecretBoundaries(t *testing.T) {
	for _, mode := range []string{"api-key", "session", "oauth"} {
		t.Run(mode, func(t *testing.T) {
			s, router, _ := testServer(t, true)
			admin, err := s.store.UserByEmail(t.Context(), "admin@localhost")
			if err != nil {
				t.Fatal(err)
			}
			f := mcpUserSession(t, s, router, admin.ID)
			createKeyProject(t, f, "secrets")
			var token string
			switch mode {
			case "api-key":
				token, _ = issueKey(t, f, "/api/v1/keys", scopeBody("all", keyScopeJSON{Project: "*", Permissions: store.KeyPermissions()}))
			case "oauth":
				client := registerMCPClient(t, router)
				code := mcpConsentCode(t, s, f, client, true)
				tokens := oauthPost(t, router, "/oauth/token", mcpCodeForm(s, client, code), http.StatusOK)
				token, _ = tokens["access_token"].(string)
			}
			request := func(method string, params any) map[string]any {
				t.Helper()
				body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
				var reply map[string]any
				if token == "" {
					status, result := f.do(http.MethodPost, "/mcp", body)
					if status != http.StatusOK {
						t.Fatal(status)
					}
					reply = result
				} else {
					rec := keyRequest(t, router, token, http.MethodPost, "/mcp", body, http.StatusOK)
					if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
						t.Fatal(err)
					}
				}
				assertSecretMasked(t, reply)
				return reply
			}
			call := func(name string, args map[string]any, want int) map[string]any {
				t.Helper()
				reply := request("tools/call", map[string]any{"name": name, "arguments": args})
				result, _ := reply["result"].(map[string]any)
				structured, _ := result["structuredContent"].(map[string]any)
				if structured["http_status"] != float64(want) {
					t.Fatalf("%s returned an unexpected status", name)
				}
				return structured
			}
			base := map[string]any{"project": "secrets", "environment": "production"}
			args := map[string]any{"project": "secrets", "environment": "production", "key": "token", "value": testSecret, "secret": true}
			call("set_config", args, http.StatusOK)
			delete(args, "secret")
			call("set_config", args, http.StatusOK)
			delete(args, "value")
			if got := call("get_config", args, http.StatusOK); got["value"] != "sk-****x123" || got["secret"] != true {
				t.Fatal("MCP lost secret protection")
			}
			for _, tool := range []string{"list_config", "list_values"} {
				want := http.StatusOK
				if mode == "session" && tool == "list_values" {
					want = http.StatusUnauthorized
				}
				call(tool, base, want)
				call(tool, map[string]any{"project": "secrets", "environment": "production", "keys": []string{"token"}}, want)
			}
			call("set_configs", map[string]any{"project": "secrets", "environment": "production", "items": []map[string]any{{"key": "token", "value": testSecret}, {"key": "new", "value": testSecret, "secret": true}}}, http.StatusNoContent)
			call("set_config", map[string]any{"project": "secrets", "environment": "production", "key": "token", "value": testSecret, "secret": false, "new_key": "renamed"}, http.StatusForbidden)
			call("set_configs", map[string]any{"project": "secrets", "environment": "production", "items": []map[string]any{{"key": "token", "value": "changed"}, {"key": "new", "value": "changed", "secret": false}}}, http.StatusForbidden)
			proposal := call("propose_config", map[string]any{"project": "secrets", "environment": "production", "key": "token", "action": "set", "value": testSecret}, http.StatusCreated)
			call("list_proposals", map[string]any{"project": "secrets"}, http.StatusOK)
			call("review_proposal", map[string]any{"project": "secrets", "id": proposal["id"], "approve": true}, http.StatusOK)
			call("get_config", args, http.StatusOK)
			list, err := json.Marshal(request("tools/list", nil))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"reveal_config", "reveal_proposal", "reveal_values"} {
				if strings.Contains(string(list), name) {
					t.Fatal("privileged read exposed in MCP discovery")
				}
				if reply := request("tools/call", map[string]any{"name": name, "arguments": args}); reply["error"] == nil {
					t.Fatal("MCP invoked privileged read")
				}
			}
			path := "/api/v1/projects/secrets/environments/production/items/token/reveal"
			if status, result := f.do(http.MethodPost, path, nil); status != http.StatusOK || result["value"] != testSecret {
				t.Fatal("MCP write did not preserve the secret")
			}
		})
	}
}

func TestSecretProposalHistory(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "secrets")
	base := "/api/v1/projects/secrets"
	status, proposal := f.do(http.MethodPost, base+"/proposals", map[string]any{"environment": "production", "key": "token", "action": "set", "value": testSecret})
	if status != http.StatusCreated {
		t.Fatal(status)
	}
	if status, _ := f.do(http.MethodPut, base+"/environments/production/items/token", map[string]any{"value": testSecret, "secret": true}); status != http.StatusOK {
		t.Fatal(status)
	}
	reader, _ := issueKey(t, f, "/api/v1/keys", scopeBody("reviewer", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ProposalsRead, store.ProposalsReview}}))
	path := fmt.Sprintf("%s/proposals/%.0f", base, proposal["id"])
	keyRequest(t, f.router, reader, http.MethodPost, path+"/reveal", nil, http.StatusForbidden)
	if status, raw := f.do(http.MethodPost, path+"/reveal", nil); status != http.StatusOK || raw["value"] != testSecret {
		t.Fatal("authorized proposal reveal failed")
	}
	review := keyRequest(t, f.router, reader, http.MethodPost, path+"/review", map[string]any{"approve": true}, http.StatusOK)
	assertSecretMasked(t, review.Body.String())
	if status, _ := f.do(http.MethodPut, base+"/environments/production/items/token", map[string]any{"key": "renamed", "description": "Keep original value"}); status != http.StatusOK {
		t.Fatal(status)
	}
	if status, _ := f.do(http.MethodDelete, base+"/environments/production/items/renamed", nil); status != http.StatusNoContent {
		t.Fatal(status)
	}
	rec := keyRequest(t, f.router, reader, http.MethodGet, base+"/proposals", nil, http.StatusOK)
	assertSecretMasked(t, rec.Body.String())
	if !strings.Contains(rec.Body.String(), "sk-****x123") {
		t.Fatal("historical proposal protection was lost")
	}
}
