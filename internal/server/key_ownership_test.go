package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OXeu/configcenter/internal/apikey"
	"github.com/OXeu/configcenter/internal/store"
)

func TestUsersManageTheirOwnScopedKeys(t *testing.T) {
	s, router, _ := testServer(t, false)
	user, err := s.store.CreateUser(t.Context(), store.User{Email: "user@example.com", Name: "User", Role: store.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateSession(t.Context(), store.Session{ID: "regular-session", UserID: user.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f := &flows{t: t, router: router, csrf: "csrf", cookies: map[string]*http.Cookie{"cc_session": {Name: "cc_session", Value: "regular-session"}}}
	createKeyProject(t, f, "owned")
	status, _ := f.do(http.MethodGet, "/keys", nil)
	if status != http.StatusOK {
		t.Fatal("key page is unavailable to ordinary users")
	}
	token, id := issueKey(t, f, "/api/v1/keys", scopeBody("mine", keyScopeJSON{Project: "owned", Permissions: []store.Permission{store.ConfigRead}}))
	keyRequest(t, router, token, http.MethodGet, "/api/v1/agent/environments/production/values", nil, http.StatusOK)
	for _, project := range []string{"*", "configcenter"} {
		status, _ = f.do(http.MethodPost, "/api/v1/keys", scopeBody("escalate", keyScopeJSON{Project: project, Permissions: []store.Permission{store.ConfigRead}}))
		if status != http.StatusForbidden {
			t.Fatalf("user granted forbidden project: %d", status)
		}
		status, _ = f.do(http.MethodPatch, fmt.Sprintf("/api/v1/keys/%d", id), scopeBody("escalate", keyScopeJSON{Project: project, Permissions: []store.Permission{store.ConfigRead}}))
		if status != http.StatusForbidden {
			t.Fatalf("user expanded scopes: %d", status)
		}
	}
	_, digest, err := apikey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.store.CreateAPIKey(t.Context(), store.APIKey{Name: "other-owner", CreatedBy: 1, Digest: digest[:], Scopes: []store.KeyScope{{Permissions: store.KeyPermissions()}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		status, _ = f.do(method, fmt.Sprintf("/api/v1/keys/%d", other.ID), scopeBody("forbidden"))
		if status != http.StatusForbidden {
			t.Fatalf("user managed another user's key: %d", status)
		}
	}
	status, listed := f.do(http.MethodGet, "/api/v1/keys", nil)
	keys, ok := listed["keys"].([]any)
	if status != http.StatusOK || !ok || len(keys) != 1 {
		t.Fatal("key list did not preserve ownership")
	}
	status, options := f.do(http.MethodGet, "/api/v1/key-scopes", nil)
	if status != http.StatusOK || options["all_projects"] != false {
		t.Fatal("ordinary user received wildcard scope option")
	}
}

func TestScopedKeyAttributionAndExplicitAuthentication(t *testing.T) {
	s, router, _ := testServer(t, false)
	admin, err := s.store.UserByEmail(t.Context(), "admin@localhost")
	if err != nil {
		t.Fatal(err)
	}
	token, digest, err := apikey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateAPIKey(t.Context(), store.APIKey{Name: "manager", Digest: digest[:], Prefix: apikey.Display(token), Scopes: []store.KeyScope{{Permissions: store.KeyPermissions()}}, CreatedBy: admin.ID}); err != nil {
		t.Fatal(err)
	}
	keyRequest(t, router, token, http.MethodPost, "/api/v1/projects", map[string]any{"slug": "audit", "name": "Audit"}, http.StatusCreated)
	project, err := s.store.ProjectBySlug(t.Context(), "audit")
	if err != nil || project.OwnerID != admin.ID {
		t.Fatal("project lost its owner")
	}
	env, err := s.store.EnvironmentBySlug(t.Context(), project.ID, "production")
	if err != nil {
		t.Fatal(err)
	}
	keyRequest(t, router, token, http.MethodPut, "/api/v1/projects/audit/environments/production/items/value", map[string]any{"value": "20"}, http.StatusOK)
	item, err := s.store.GetItem(t.Context(), project.ID, env.ID, "value")
	if err != nil || item.UpdatedBy != admin.ID {
		t.Fatal("configuration lost its actor")
	}
	if err := s.store.CreateSession(t.Context(), store.Session{ID: "admin-session", UserID: admin.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		credential string
		status     int
	}{{token, http.StatusOK}, {"cc_invalid", http.StatusUnauthorized}} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/audit/environments/production/items/value", strings.NewReader(`{"value":"30"}`))
		req.AddCookie(&http.Cookie{Name: "cc_session", Value: "admin-session"})
		req.Header.Set("Authorization", "Bearer "+tc.credential)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("explicit authentication: %d, want %d", rec.Code, tc.status)
		}
	}
}
