package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/internal/store"
)

func conditionalRequest(t *testing.T, router http.Handler, token, path, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestConfigurationReadRevalidatesWithETag(t *testing.T) {
	f := newFlows(t)
	if status, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]string{"slug": "cache", "name": "Cache"}); status != http.StatusCreated {
		t.Fatal(status)
	}
	base := "/api/v1/projects/cache/environments/production/items"
	if status, _ := f.do(http.MethodPut, base+"/db.pool", map[string]string{"value": "10"}); status != http.StatusOK {
		t.Fatal(status)
	}
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("reader", keyScopeJSON{Project: "cache", Permissions: []store.Permission{store.ConfigRead}}))
	for _, path := range []string{base, "/api/v1/agent/environments/production/values"} {
		initial := conditionalRequest(t, f.router, token, path, "")
		if initial.Code != http.StatusOK {
			t.Fatalf("initial read: %d", initial.Code)
		}
		etag := initial.Header().Get("ETag")
		if etag == "" || !strings.HasPrefix(etag, `"`) {
			t.Fatalf("missing strong validator: %q", etag)
		}
		unchanged := conditionalRequest(t, f.router, token, path, etag)
		if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
			t.Fatalf("unchanged read was not revalidated: %d %q", unchanged.Code, unchanged.Body.String())
		}
		if weak := conditionalRequest(t, f.router, token, path, "W/"+etag); weak.Code != http.StatusNotModified {
			t.Fatalf("weak validator rejected: %d", weak.Code)
		}
		if stale := conditionalRequest(t, f.router, token, path, `"stale"`); stale.Code != http.StatusOK {
			t.Fatalf("unknown validator served 304: %d", stale.Code)
		}
		if status, _ := f.do(http.MethodPut, base+"/db.pool", map[string]string{"value": "20"}); status != http.StatusOK {
			t.Fatal(status)
		}
		changed := conditionalRequest(t, f.router, token, path, etag)
		if changed.Code != http.StatusOK || changed.Header().Get("ETag") == etag || !strings.Contains(changed.Body.String(), `"value":"20"`) {
			t.Fatalf("changed configuration reused the cached revision: %d %q", changed.Code, changed.Body.String())
		}
		if status, _ := f.do(http.MethodPut, base+"/db.pool", map[string]string{"value": "10"}); status != http.StatusOK {
			t.Fatal(status)
		}
	}
}

func TestMaskedSecretRevealKeepsConditionalDisabled(t *testing.T) {
	f := newFlows(t)
	if status, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]string{"slug": "secrets", "name": "Secrets"}); status != http.StatusCreated {
		t.Fatal(status)
	}
	base := "/api/v1/projects/secrets/environments/production/items"
	if status, _ := f.do(http.MethodPut, base+"/token", map[string]any{"value": "sk-confidential-x123", "secret": true}); status != http.StatusOK {
		t.Fatal(status)
	}
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("privileged", keyScopeJSON{Project: "secrets", Permissions: []store.Permission{store.ConfigReadSecrets}}))
	path := "/api/v1/agent/environments/production/values/reveal"
	initial := conditionalRequest(t, f.router, token, path, "")
	if initial.Code != http.StatusOK || initial.Header().Get("Cache-Control") != "no-store" || initial.Header().Get("ETag") != "" {
		t.Fatalf("privileged read was cacheable: %d %v", initial.Code, initial.Header())
	}
	if again := conditionalRequest(t, f.router, token, path, "*"); again.Code != http.StatusOK {
		t.Fatalf("privileged read honoured a conditional request: %d", again.Code)
	}
}
