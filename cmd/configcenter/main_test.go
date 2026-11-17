package main

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/OXeu/configcenter/internal/server"
	"github.com/OXeu/configcenter/internal/store"
)

func TestStartupLoadsDatabaseWithoutAPI(t *testing.T) {
	values := startupValues(t)
	seed, err := store.OpenDatabase(t.Context(), "sqlite", values["CC_DB"], "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, seed)
	project, err := seed.ProjectBySlug(t.Context(), "configcenter")
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"CC_ADDR": "127.0.0.1:9090", "CC_PUBLIC_URL": "https://config.example",
		"CC_OAUTH_ISSUER": "https://auth.xeu.life", "CC_OAUTH_CLIENT_SECRET": "stored-secret",
		"CC_DB": "invalid-stored-database", "CC_DB_DRIVER": "mysql",
	} {
		secret := key == "CC_OAUTH_CLIENT_SECRET"
		if _, err := seed.PutItem(t.Context(), store.ItemInput{
			ProjectID: project.ID, EnvironmentID: project.PrimaryEnvironmentID,
			Key: key, Value: value, Secret: &secret,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer api.Close()
	values["CC_CONFIGCENTER_URL"] = api.URL
	values["CC_CONFIGCENTER_API_KEY"] = "obsolete-key"
	values["CC_OAUTH_ISSUER"] = "https://github.com"
	cfg, st, err := loadConfiguration(t.Context(), func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, st)
	if requests.Load() != 0 {
		t.Fatal("startup called its HTTP API")
	}
	if cfg.OAuth.IssuerURL != "https://auth.xeu.life" || cfg.OAuth.ClientSecret != "stored-secret" || cfg.Addr != "127.0.0.1:9090" || cfg.PublicURL != "https://config.example" {
		t.Fatal("startup did not resolve runtime settings from the database")
	}
	if cfg.DatabaseDriver != "sqlite" || cfg.DatabaseDSN != values["CC_DB"] {
		t.Fatal("stored settings redirected the bootstrap database")
	}
	keys, err := st.ListAPIKeys(t.Context())
	if err != nil || len(keys) != 0 {
		t.Fatal("startup required an API key", err)
	}
}

func TestFreshStartupAndServerShareDatabase(t *testing.T) {
	values := startupValues(t)
	values["CC_PUBLIC_URL"] = "https://local.example"
	cfg, st, err := loadConfiguration(t.Context(), func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, st)
	if cfg.PublicURL != "https://local.example" {
		t.Fatal("fresh database did not use local fallback")
	}
	if _, err := st.ProjectBySlug(t.Context(), "configcenter"); err != nil {
		t.Fatal("startup did not initialize its system project", err)
	}
	cfg.DatabaseDSN = "invalid-unused-dsn"
	engine, err := server.Engine(t.Context(), cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("server tried to open another database", err)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login", http.NoBody))
	if recorder.Code != http.StatusOK {
		t.Fatalf("fresh server login page returned %d", recorder.Code)
	}
}

func TestInvalidStoredConfigurationClosesBootstrapConnection(t *testing.T) {
	values := startupValues(t)
	seed, err := store.OpenDatabase(t.Context(), "sqlite", values["CC_DB"], "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore(t, seed) })
	project, err := seed.ProjectBySlug(t.Context(), "configcenter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.PutItem(t.Context(), store.ItemInput{
		ProjectID: project.ID, EnvironmentID: project.PrimaryEnvironmentID, Key: "CC_ADDR", Value: "invalid-address",
	}); err != nil {
		t.Fatal(err)
	}
	lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	if _, st, err := loadConfiguration(t.Context(), lookup); err == nil || st != nil {
		t.Fatal("invalid stored configuration did not fail startup")
	}
	closeStore(t, seed)
	_, st, err := loadConfiguration(t.Context(), lookup)
	if err != nil {
		t.Fatal("failed startup leaked a connection retaining the invalid in-memory database", err)
	}
	closeStore(t, st)
}

func startupValues(t *testing.T) map[string]string {
	t.Helper()
	missing, err := filepath.Abs(filepath.Join("..", "..", ".tmp", "missing-startup-"+rand.Text()+".env"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"CC_ENV_FILE": missing,
		"CC_DB":       "file:startup-" + rand.Text() + "?mode=memory&cache=shared",
	}
}

func closeStore(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.Close(); err != nil {
		t.Error(err)
	}
}
