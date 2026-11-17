package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/internal/store"
)

func TestEnvironmentRenameAndDeletion(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "billing")
	createKeyProject(t, f, "other")
	base := "/api/v1/projects/billing"
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("manager", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.EnvironmentsRead, store.EnvironmentsWrite, store.EnvironmentsDelete, store.ConfigRead, store.ConfigWrite, store.ProposalsRead, store.ProposalsWrite}}))
	keyRequest(t, f.router, token, http.MethodPost, base+"/environments", map[string]string{"slug": "staging", "name": "Staging"}, http.StatusCreated)
	keyRequest(t, f.router, token, http.MethodPut, base+"/environments/staging/items/keep", map[string]string{"value": "retained"}, http.StatusOK)
	keyRequest(t, f.router, token, http.MethodPost, base+"/proposals", map[string]string{"environment": "staging", "key": "pending", "action": "set", "value": "proposal"}, http.StatusCreated)
	rename := map[string]string{"slug": "preview", "name": "Preview"}
	reader, _ := issueKey(t, f, "/api/v1/keys", scopeBody("reader", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.EnvironmentsRead}}))
	keyRequest(t, f.router, reader, http.MethodPatch, base+"/environments/staging", rename, http.StatusForbidden)
	keyRequest(t, f.router, reader, http.MethodDelete, base+"/environments/staging", nil, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodPatch, "/api/v1/projects/other/environments/production", rename, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodDelete, "/api/v1/projects/other/environments/production", nil, http.StatusForbidden)
	for _, invalid := range []map[string]string{{"slug": "bad slug", "name": "Name"}, {"slug": "preview", "name": " "}, {"name": "Missing slug"}} {
		keyRequest(t, f.router, token, http.MethodPatch, base+"/environments/staging", invalid, http.StatusBadRequest)
	}
	keyRequest(t, f.router, token, http.MethodPatch, base+"/environments/staging", map[string]string{"slug": "production", "name": "Conflict"}, http.StatusConflict)
	keyRequest(t, f.router, token, http.MethodPatch, base+"/environments/staging", rename, http.StatusOK)
	keyRequest(t, f.router, token, http.MethodGet, base+"/environments/staging/items/keep", nil, http.StatusNotFound)
	if status, item := f.do(http.MethodGet, base+"/environments/preview/items/keep", nil); status != http.StatusOK || item["value"] != "retained" {
		t.Fatal("renaming lost configuration", status, item)
	}
	_, result := f.do(http.MethodGet, base+"/proposals", nil)
	proposals, ok := result["proposals"].([]any)
	if !ok || len(proposals) != 1 {
		t.Fatal("renaming lost proposal", result)
	}
	proposal, ok := proposals[0].(map[string]any)
	if !ok || proposal["environment"] != "preview" {
		t.Fatal("proposal environment not renamed", proposal)
	}
	keyRequest(t, f.router, token, http.MethodDelete, base+"/environments/preview", nil, http.StatusNoContent)
	keyRequest(t, f.router, token, http.MethodGet, base+"/environments/preview/items/keep", nil, http.StatusNotFound)
	_, result = f.do(http.MethodGet, base+"/proposals", nil)
	proposals, ok = result["proposals"].([]any)
	if !ok || len(proposals) != 0 {
		t.Fatal("deletion retained proposals", result)
	}
	keyRequest(t, f.router, token, http.MethodDelete, base+"/environments/production", nil, http.StatusUnprocessableEntity)
	f.csrf = ""
	if status, _ := f.do(http.MethodPatch, base+"/environments/production", rename); status != http.StatusForbidden {
		t.Fatal("missing CSRF accepted", status)
	}
	if status, _ := f.do(http.MethodDelete, base+"/environments/production", nil); status != http.StatusForbidden {
		t.Fatal("missing CSRF accepted", status)
	}
}

func TestSystemEnvironmentProtection(t *testing.T) {
	f := newFlows(t)
	path := "/api/v1/projects/configcenter/environments/production"
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		if status, _ := f.do(method, path, map[string]string{"slug": "renamed", "name": "Renamed"}); status != http.StatusUnprocessableEntity {
			t.Fatal("system environment changed", method, status)
		}
	}
	if status, _ := f.do(http.MethodPatch, path, map[string]string{"slug": "production", "name": "System"}); status != http.StatusOK {
		t.Fatal("display name update failed", status)
	}
	if status, _ := f.do(http.MethodGet, "/api/v1/settings", nil); status != http.StatusOK {
		t.Fatal("settings unavailable", status)
	}
}

func TestPrimaryEnvironmentPageAndPermissions(t *testing.T) {
	f := newFlows(t)
	createKeyProject(t, f, "billing")
	createKeyProject(t, f, "other")
	base := "/api/v1/projects/billing/environments"
	token, _ := issueKey(t, f, "/api/v1/keys", scopeBody("manager", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.EnvironmentsRead, store.EnvironmentsWrite, store.EnvironmentsDelete}}))
	reader, _ := issueKey(t, f, "/api/v1/keys", scopeBody("reader", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.EnvironmentsRead}}))
	keyRequest(t, f.router, token, http.MethodPost, base, map[string]string{"slug": "alpha", "name": "Alpha"}, http.StatusCreated)
	assertPageEnvironment(t, f, "production")
	keyRequest(t, f.router, reader, http.MethodPost, base+"/alpha/primary", nil, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodPost, "/api/v1/projects/other/environments/production/primary", nil, http.StatusForbidden)
	keyRequest(t, f.router, token, http.MethodPost, base+"/missing/primary", nil, http.StatusNotFound)
	for range 2 {
		keyRequest(t, f.router, token, http.MethodPost, base+"/alpha/primary", nil, http.StatusOK)
	}
	assertPageEnvironment(t, f, "alpha")
	response := keyRequest(t, f.router, token, http.MethodGet, base, nil, http.StatusOK)
	var list struct {
		Environments []envJSON `json:"environments"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Environments) != 2 || !list.Environments[0].IsPrimary || list.Environments[1].IsPrimary {
		t.Fatal("primary flags incorrect", list)
	}
	keyRequest(t, f.router, token, http.MethodDelete, base+"/alpha", nil, http.StatusUnprocessableEntity)
	keyRequest(t, f.router, token, http.MethodPatch, base+"/alpha", map[string]string{"slug": "preview", "name": "Preview"}, http.StatusOK)
	keyRequest(t, f.router, token, http.MethodPost, base, map[string]string{"slug": "aardvark", "name": "First alphabetically"}, http.StatusCreated)
	assertPageEnvironment(t, f, "preview")
	keyRequest(t, f.router, token, http.MethodDelete, base+"/production", nil, http.StatusNoContent)
	assertPageEnvironment(t, f, "preview")
	f.csrf = ""
	if status, _ := f.do(http.MethodPost, base+"/aardvark/primary", nil); status != http.StatusForbidden {
		t.Fatal("missing CSRF accepted", status)
	}
	assertPageEnvironment(t, f, "preview")
}

func assertPageEnvironment(t *testing.T, f *flows, expected string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/projects/billing", http.NoBody)
	req.Header.Set("Cookie", "cc_session="+f.cookies["cc_session"].Value)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatal("project page unavailable", response.Code)
	}
	_, tail, found := strings.Cut(response.Body.String(), `<script id="page-data" type="application/json">`)
	if !found {
		t.Fatal("missing page data")
	}
	raw, _, found := strings.Cut(tail, "</script>")
	if !found {
		t.Fatal("invalid page data")
	}
	var page struct {
		Env string `json:"env"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	if page.Env != expected {
		t.Fatalf("default environment = %q, want %q", page.Env, expected)
	}
}
