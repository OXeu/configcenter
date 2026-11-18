package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadUsesEnvironmentPathAndKey(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/environments/production/values", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer cc_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if got := r.URL.Query().Get("prefix"); got != "db." {
			http.Error(w, "unexpected prefix", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Item{
			{Key: "db.pool", Value: "20"},
			{Key: "db.timeout", Value: "5s"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := New(Config{DotenvFiles: []string{}, BaseURL: srv.URL, APIKey: "cc_test"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.LoadPrefix(context.Background(), "db."); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := client.Int("db.pool", 1); got != 20 {
		t.Fatalf("db.pool = %d", got)
	}
	if got := client.GetDefault("missing", "fallback"); got != "fallback" {
		t.Fatalf("missing = %q", got)
	}
}

func TestMissingProjectIsNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	var reported error
	client, err := New(Config{DotenvFiles: []string{}, BaseURL: srv.URL, APIKey: "cc_test", Environment: "staging", OnError: func(err error) { reported = err }})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.Load(t.Context()); err != nil || !errors.Is(reported, ErrNotFound) {
		t.Fatal("404 was not reported separately from startup loading", err, reported)
	}
}

func TestNewRejectsUnsafeConfig(t *testing.T) {
	t.Parallel()
	cases := []Config{
		{APIKey: "cc_x"},
		{BaseURL: "http://example.com"},
		{BaseURL: "http://example.com", APIKey: "nope"},
	}
	for _, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Fatalf("config %+v was accepted", cfg)
		}
	}
}
