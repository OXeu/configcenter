package configcenter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEnsureDefaultsRemoteUsesBatchReadAndWrite(t *testing.T) {
	values := map[string]string{"keep": "operator-value"}
	var mu sync.Mutex
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/agent/environments/production/values":
			items := []Item{}
			for key, value := range values {
				items = append(items, Item{Key: key, Value: value})
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items})
		case "/api/v1/agent/project":
			json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "app"}})
		case "/api/v1/projects/app/environments/production/items":
			if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer cc_test" {
				t.Error("invalid initializer request")
			}
			var in struct {
				Items []Item `json:"items"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			for _, item := range in.Items {
				values[item.Key] = item.Value
				writes++
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unsafe initialization endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}, Defaults: map[string]string{"new": "fallback"}})
	defaults := map[string]string{"keep": "default", "new": "new-default"}
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("new", "") != "fallback" {
		t.Fatal("missing fallback before initialization")
	}
	if err := client.EnsureDefaults(t.Context(), defaults); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("keep", "") != "operator-value" || client.GetDefault("new", "") != "new-default" {
		t.Fatal("defaults replaced values or failed to refresh")
	}
	if err := client.EnsureDefaults(t.Context(), defaults); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 1 {
		t.Fatal("unchanged settings rewritten", writes)
	}
}

func TestEnsureDefaultsErrorsIdentifyKeys(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/agent/environments/production/values":
					json.NewEncoder(w).Encode(map[string]any{"items": []Item{}})
				case "/api/v1/agent/project":
					json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "app"}})
				default:
					w.WriteHeader(status)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}})
			err := client.EnsureDefaults(t.Context(), map[string]string{"certcenter.server": "secret-value"})
			if err == nil || !strings.Contains(err.Error(), "certcenter.server") || strings.Contains(err.Error(), "secret-value") {
				t.Fatal("missing key context or leaked value", err)
			}
		})
	}
}

func TestEnsureDefaultsLocalPreservesExistingValues(t *testing.T) {
	client := testClient(t, Config{DotenvFiles: []string{}})
	if err := client.Put(t.Context(), "keep", "operator-value"); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDefaults(t.Context(), map[string]string{"keep": "default", "new": "initialized"}); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("keep", "") != "operator-value" || client.GetDefault("new", "") != "initialized" {
		t.Fatal("local initialization lost values")
	}
}
