package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

func TestLoadPrefersRemoteConfiguration(t *testing.T) {
	remote := configRemote(t, "staging", map[string]string{
		"CC_PUBLIC_URL":          "https://config.example/",
		"CC_OAUTH_ISSUER":        "https://auth.xeu.life/",
		"CC_OAUTH_CLIENT_ID":     "authcenter-client",
		"CC_OAUTH_CLIENT_SECRET": "remote-client-secret",
		"CC_OAUTH_SCOPES":        "profile email",
		"CC_SESSION_SECRET":      "remote-session-secret",
		"CC_LANGUAGES":           "zh-CN,en",
		"CC_ENV":                 "production",
	})
	file := configFile(t, "CC_CONFIGCENTER_URL="+remote.URL+"\n"+
		"CC_CONFIGCENTER_API_KEY=cc_file\nCC_CONFIGCENTER_ENVIRONMENT=staging\n"+
		"CC_ADDR=127.0.0.1:9999\nCC_BOOTSTRAP_ADMIN_NAME=file-admin\n"+
		"CC_OAUTH_ISSUER=https://github.com/login/oauth\nCC_OAUTH_SCOPES=read:user user:email\n")
	cfg, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":             file,
		"CC_CONFIGCENTER_API_KEY": "cc_test",
		"CC_PUBLIC_URL":           "http://local.example",
		"CC_OAUTH_ISSUER":         "https://github.com",
		"CC_OAUTH_CLIENT_SECRET":  "local-client-secret",
		"CC_SESSION_SECRET":       "local-session-secret",
		"CC_ADDR":                 "127.0.0.1:7777",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.IssuerURL != "https://auth.xeu.life" || cfg.OAuth.ClientID != "authcenter-client" || strings.Join(cfg.OAuth.Scopes, " ") != "profile email" {
		t.Fatal("remote OAuth settings did not override local settings")
	}
	if cfg.OAuth.ClientSecret != "remote-client-secret" || cfg.SessionSecret != "remote-session-secret" {
		t.Fatal("secret settings did not use the privileged remote snapshot")
	}
	if cfg.PublicURL != "https://config.example" || cfg.OAuth.RedirectURL != "https://config.example/oauth/callback" {
		t.Fatal("callback did not use the remote public URL")
	}
	if cfg.Addr != "127.0.0.1:7777" || cfg.Bootstrap.AdminName != "file-admin" || cfg.DatabaseDriver != "sqlite" {
		t.Fatal("missing remote keys did not fall back to environment, dotenv and defaults")
	}
	if strings.Join(cfg.Languages, ",") != "zh-CN,en" {
		t.Fatal("languages did not use the remote snapshot")
	}
}

func TestLoadPreservesEmptyRemoteValues(t *testing.T) {
	remote := configRemote(t, "production", map[string]string{
		"CC_OAUTH_ISSUER":        "",
		"CC_OAUTH_CLIENT_SECRET": "",
		"CC_OAUTH_SCOPES":        "",
		"CC_OAUTH_REDIRECT_URL":  "",
		"CC_PUBLIC_URL":          "https://config.example",
	})
	cfg, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":             configFile(t, ""),
		"CC_CONFIGCENTER_URL":     remote.URL,
		"CC_CONFIGCENTER_API_KEY": "cc_test",
		"CC_OAUTH_ISSUER":         "https://github.com",
		"CC_OAUTH_CLIENT_SECRET":  "local-secret",
		"CC_OAUTH_SCOPES":         "read:user user:email",
		"CC_OAUTH_REDIRECT_URL":   "http://local.example/oauth/callback",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.IssuerURL != "" || cfg.OAuth.ClientSecret != "" || len(cfg.OAuth.Scopes) != 0 {
		t.Fatal("empty remote settings restored local OAuth values")
	}
	if cfg.OAuth.RedirectURL != "https://config.example/oauth/callback" {
		t.Fatal("empty remote callback did not restore automatic derivation")
	}
}

func TestLoadRemoteFailureWarnsAndFallsBack(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private-response-value", http.StatusServiceUnavailable)
	}))
	defer remote.Close()
	cfg, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":             configFile(t, "CC_PUBLIC_URL=https://fallback.example\n"),
		"CC_CONFIGCENTER_URL":     remote.URL,
		"CC_CONFIGCENTER_API_KEY": "cc_private-token",
		"CC_ADDR":                 "127.0.0.1:9010",
		"CC_SESSION_SECRET":       "private-session-secret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9010" || cfg.PublicURL != "https://fallback.example" || cfg.DatabaseDriver != "sqlite" {
		t.Fatal("unavailable remote prevented local startup")
	}
	if !strings.Contains(logs.String(), "WARN") || !strings.Contains(logs.String(), "using local configuration") {
		t.Fatal("remote failure was not reported")
	}
	if strings.Contains(logs.String(), "private") {
		t.Fatal("configuration warning exposed a secret or response body")
	}
}

func TestLoadRejectsInvalidRemoteConfiguration(t *testing.T) {
	remote := configRemote(t, "production", map[string]string{
		"CC_ADDR": "invalid-address",
	})
	_, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":             configFile(t, ""),
		"CC_CONFIGCENTER_URL":     remote.URL,
		"CC_CONFIGCENTER_API_KEY": "cc_test",
		"CC_ADDR":                 "127.0.0.1:8080",
	}))
	if err == nil {
		t.Fatal("invalid remote configuration was replaced with valid local settings")
	}
}

func TestLoadRequiresRemoteSessionSecretInProduction(t *testing.T) {
	remote := configRemote(t, "production", map[string]string{
		"CC_ENV":            "production",
		"CC_SESSION_SECRET": "",
	})
	_, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":             configFile(t, ""),
		"CC_CONFIGCENTER_URL":     remote.URL,
		"CC_CONFIGCENTER_API_KEY": "cc_test",
		"CC_SESSION_SECRET":       "old-local-secret",
	}))
	if err == nil {
		t.Fatal("empty remote production secret fell back to a local secret")
	}
}

func TestLoadRejectsIncompleteRemoteConnection(t *testing.T) {
	for _, values := range []map[string]string{
		{"CC_CONFIGCENTER_URL": "https://config.example"},
		{"CC_CONFIGCENTER_API_KEY": "cc_test"},
		{"CC_CONFIGCENTER_URL": "https://config.example", "CC_CONFIGCENTER_API_KEY": "invalid"},
	} {
		values["CC_ENV_FILE"] = configFile(t, "")
		if _, err := Load(t.Context(), configLookup(values)); err == nil {
			t.Fatal("invalid bootstrap settings were accepted")
		}
	}
}

func TestLoadCancellationDoesNotFallBack(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	remote := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer remote.Close()
	_, err := Load(ctx, configLookup(map[string]string{
		"CC_ENV_FILE":             configFile(t, ""),
		"CC_CONFIGCENTER_URL":     remote.URL,
		"CC_CONFIGCENTER_API_KEY": "cc_test",
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestLoadRejectsInvalidDotenvWithoutLeakingValues(t *testing.T) {
	_, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE": configFile(t, "CC_SESSION_SECRET=\"private-session-secret\n"),
	}))
	if !errors.Is(err, configcenter.ErrInvalidDotenv) {
		t.Fatalf("expected invalid dotenv, got %v", err)
	}
	if strings.Contains(err.Error(), "private-session-secret") {
		t.Fatal("invalid dotenv exposed a secret value")
	}
}

func configRemote(t *testing.T, environment string, values map[string]string) *httptest.Server {
	t.Helper()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/agent/environments/"+environment+"/values/reveal" {
			t.Errorf("unexpected configuration request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("prefix") != "CC_" || r.Header.Get("Authorization") != "Bearer cc_test" {
			t.Error("configuration request did not use the expected prefix and project key")
		}
		items := make([]configcenter.Item, 0, len(values))
		for key, value := range values {
			items = append(items, configcenter.Item{Key: key, Value: value, Secret: strings.Contains(key, "SECRET")})
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"items": items}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(remote.Close)
	return remote
}
