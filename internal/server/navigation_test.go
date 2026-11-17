package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNavigationPageData(t *testing.T) {
	f := newFlows(t)
	for _, path := range []string{"/projects", "/keys", "/settings", "/users"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			req.Header.Set("X-Center-Navigation", "1")
			req.Header.Set("Accept", "application/json")
			for _, cookie := range f.cookies {
				req.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			f.router.ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("page headers = %v", response.Header())
			}
			var payload struct {
				Title string `json:"title"`
				Data  struct {
					Page     string `json:"page"`
					CSRF     string `json:"csrf"`
					Nonce    string `json:"nonce"`
					LoggedIn bool   `json:"loggedIn"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode page: %v", err)
			}
			if payload.Title == "" || payload.Data.Page == "" || !payload.Data.LoggedIn || payload.Data.CSRF != f.csrf {
				t.Fatalf("incomplete page response: %+v", payload)
			}
			if payload.Data.Nonce == "" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "'nonce-"+payload.Data.Nonce+"'") {
				t.Fatal("page nonce differs from its response CSP")
			}
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/keys", http.NoBody)
	req.Header.Set("X-Center-Navigation", "1")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated navigation bypassed authorization: %d", response.Code)
	}
}
