package config

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsEnvFileWithoutOverridingExportedValues(t *testing.T) {
	file := configFile(t, "CC_ADDR=0.0.0.0:9999\nCC_PUBLIC_URL=http://files.example/\nCC_OAUTH_ISSUER=https://auth.xeu.life/\n")
	before, present := os.LookupEnv("CC_PUBLIC_URL")
	cfg, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE": file,
		"CC_ADDR":     "127.0.0.1:7777",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:7777" {
		t.Fatalf("exported CC_ADDR was overridden: %q", cfg.Addr)
	}
	if cfg.PublicURL != "http://files.example" {
		t.Fatalf("file value not used or not trimmed: %q", cfg.PublicURL)
	}
	if cfg.OAuth.IssuerURL != "https://auth.xeu.life" {
		t.Fatalf("issuer trailing slash not trimmed: %q", cfg.OAuth.IssuerURL)
	}
	if cfg.OAuth.RedirectURL != "http://files.example/oauth/callback" {
		t.Fatalf("redirect not derived: %q", cfg.OAuth.RedirectURL)
	}
	if after, ok := os.LookupEnv("CC_PUBLIC_URL"); after != before || ok != present {
		t.Fatal("loading configuration changed the process environment")
	}
}

func TestLoadToleratesMissingEnvFile(t *testing.T) {
	cfg, err := Load(t.Context(), configLookup(map[string]string{
		"CC_ENV_FILE": configFile(t, "") + ".absent",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Addr == "" {
		t.Fatal("defaults were not applied")
	}
}

func TestDatabaseDrivers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		driver string
		dsn    string
		valid  bool
	}{
		{"default", "", "", true},
		{"mysql", "mysql", "user:password@tcp(localhost:3306)/configcenter", true},
		{"mysql missing dsn", "mysql", "", false},
		{"mysql missing database", "mysql", "user:password@tcp(localhost:3306)/", false},
		{"unknown", "postgres", "", false},
		{"sqlite invalid", "sqlite", "invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"CC_DB_DRIVER": tc.driver, "CC_DB": tc.dsn, "CC_ENV_FILE": "missing-test-env"}
			cfg, err := Load(t.Context(), configLookup(values))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.name == "default" && cfg.DatabaseDriver != "sqlite" {
				t.Fatal("default must remain sqlite")
			}
			if tc.name == "mysql" && !strings.Contains(cfg.DatabaseDSN, "parseTime=true") {
				t.Fatal("mysql must parse timestamps")
			}
		})
	}
}

func configLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func configFile(t *testing.T, contents string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", ".tmp", "config-test-"+rand.Text()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	file := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}
