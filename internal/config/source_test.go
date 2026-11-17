package config

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

func TestLoadPrefersStoredConfiguration(t *testing.T) {
	file := configFile(t, "CC_ADDR=127.0.0.1:9999\nCC_BOOTSTRAP_ADMIN_NAME=file-admin\n"+
		"CC_OAUTH_ISSUER=https://github.com/login/oauth\nCC_OAUTH_SCOPES=read:user user:email\n")
	loader, err := Prepare(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE":            file,
		"CC_PUBLIC_URL":          "http://local.example",
		"CC_OAUTH_ISSUER":        "https://github.com",
		"CC_OAUTH_CLIENT_SECRET": "local-client-secret",
		"CC_SESSION_SECRET":      "local-session-secret",
		"CC_ADDR":                "127.0.0.1:7777",
	}))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loader.Load(t.Context(), configcenter.MapSource{
		"CC_PUBLIC_URL":          "https://config.example/",
		"CC_OAUTH_ISSUER":        "https://auth.xeu.life/",
		"CC_OAUTH_CLIENT_ID":     "authcenter-client",
		"CC_OAUTH_CLIENT_SECRET": "stored-client-secret",
		"CC_OAUTH_SCOPES":        "profile email",
		"CC_SESSION_SECRET":      "stored-session-secret",
		"CC_LANGUAGES":           "zh-CN,en",
		"CC_ENV":                 "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.IssuerURL != "https://auth.xeu.life" || cfg.OAuth.ClientID != "authcenter-client" || strings.Join(cfg.OAuth.Scopes, " ") != "profile email" {
		t.Fatal("stored OAuth settings did not override local settings")
	}
	if cfg.OAuth.ClientSecret != "stored-client-secret" || cfg.SessionSecret != "stored-session-secret" {
		t.Fatal("secret settings did not use the stored snapshot")
	}
	if cfg.PublicURL != "https://config.example" || cfg.OAuth.RedirectURL != "https://config.example/oauth/callback" {
		t.Fatal("callback did not use the stored public URL")
	}
	if cfg.Addr != "127.0.0.1:7777" || cfg.Bootstrap.AdminName != "file-admin" || cfg.DatabaseDriver != "sqlite" {
		t.Fatal("missing stored keys did not fall back to environment, dotenv and defaults")
	}
	if strings.Join(cfg.Languages, ",") != "zh-CN,en" {
		t.Fatal("languages did not use the stored snapshot")
	}
}

func TestLoadPreservesEmptyStoredValues(t *testing.T) {
	loader := testLoader(t, map[string]string{
		"CC_OAUTH_ISSUER":        "https://github.com",
		"CC_OAUTH_CLIENT_SECRET": "local-secret",
		"CC_OAUTH_SCOPES":        "read:user user:email",
		"CC_OAUTH_REDIRECT_URL":  "http://local.example/oauth/callback",
	})
	cfg, err := loader.Load(t.Context(), configcenter.MapSource{
		"CC_OAUTH_ISSUER":        "",
		"CC_OAUTH_CLIENT_SECRET": "",
		"CC_OAUTH_SCOPES":        "",
		"CC_OAUTH_REDIRECT_URL":  "",
		"CC_PUBLIC_URL":          "https://config.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.IssuerURL != "" || cfg.OAuth.ClientSecret != "" || len(cfg.OAuth.Scopes) != 0 {
		t.Fatal("empty stored settings restored local OAuth values")
	}
	if cfg.OAuth.RedirectURL != "https://config.example/oauth/callback" {
		t.Fatal("empty stored callback did not restore automatic derivation")
	}
}

func TestLoadKeepsBootstrapSettingsLocal(t *testing.T) {
	loader := testLoader(t, map[string]string{
		"CC_DB":                    "file:bootstrap?mode=memory&cache=shared",
		"CC_BOOTSTRAP_ADMIN_EMAIL": "local@example.com",
		"CC_BOOTSTRAP_ADMIN_NAME":  "local-admin",
	})
	cfg, err := loader.Load(t.Context(), configcenter.MapSource{
		"CC_DB_DRIVER":             "mysql",
		"CC_DB":                    "invalid-stored-database",
		"CC_BOOTSTRAP_ADMIN_EMAIL": "stored@example.com",
		"CC_BOOTSTRAP_ADMIN_NAME":  "stored-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseDriver != "sqlite" || cfg.DatabaseDSN != loader.DatabaseDSN || cfg.Bootstrap != loader.Bootstrap {
		t.Fatal("stored settings changed the bootstrap database or administrator")
	}
}

func TestLoadValidatesOnlyResolvedRuntimeConfiguration(t *testing.T) {
	loader := testLoader(t, map[string]string{"CC_ADDR": "invalid-local-address"})
	if _, err := loader.Load(t.Context(), configcenter.MapSource{"CC_ADDR": "127.0.0.1:8080"}); err != nil {
		t.Fatal("invalid local fallback prevented valid stored configuration", err)
	}
	loader = testLoader(t, map[string]string{"CC_ADDR": "127.0.0.1:8080"})
	if _, err := loader.Load(t.Context(), configcenter.MapSource{"CC_ADDR": "invalid-stored-address"}); err == nil {
		t.Fatal("invalid stored configuration was replaced with local settings")
	}
	loader = testLoader(t, map[string]string{"CC_SESSION_SECRET": "old-local-secret"})
	if _, err := loader.Load(t.Context(), configcenter.MapSource{"CC_ENV": "production", "CC_SESSION_SECRET": ""}); err == nil {
		t.Fatal("empty stored production secret fell back to a local secret")
	}
}

func TestLoadSourceFailureAndCancellationDoNotFallBack(t *testing.T) {
	loader := testLoader(t, map[string]string{})
	failure := errors.New("database unavailable")
	if _, err := loader.Load(t.Context(), failedSource{err: failure}); !errors.Is(err, failure) {
		t.Fatalf("expected database failure, got %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := loader.Load(ctx, configcenter.MapSource{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestPrepareRejectsInvalidDotenvWithoutLeakingValues(t *testing.T) {
	_, err := Prepare(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE": configFile(t, "CC_SESSION_SECRET=\"private-session-secret\n"),
	}))
	if !errors.Is(err, configcenter.ErrInvalidDotenv) {
		t.Fatalf("expected invalid dotenv, got %v", err)
	}
	if strings.Contains(err.Error(), "private-session-secret") {
		t.Fatal("invalid dotenv exposed a secret value")
	}
}

func testLoader(t *testing.T, values map[string]string) *Loader {
	t.Helper()
	values["CC_ENV_FILE"] = configFile(t, "")
	loader, err := Prepare(t.Context(), configLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

type failedSource struct{ err error }

func (s failedSource) Read(context.Context, string) ([]configcenter.Item, error) {
	return nil, s.err
}
