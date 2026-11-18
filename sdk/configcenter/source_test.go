package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func dotenvTestFile(t *testing.T, contents string) string {
	t.Helper()
	path := scratchTestPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func snapshotValues(items []Item) map[string]string {
	values := make(map[string]string, len(items))
	for _, item := range items {
		values[item.Key] = item.Value
	}
	return values
}

func TestDotenvPriorityReloadAndErrors(t *testing.T) {
	t.Setenv("SDK_ENV_UNCHANGED", "process")
	first := dotenvTestFile(t, "export SDK_ENV_UNCHANGED=first\nBASE=hello\nexpanded=\"${BASE} world\"\nempty=\n")
	second := dotenvTestFile(t, "SDK_ENV_UNCHANGED=second\nempty=second\nother=value\n")
	client := testClient(t, Config{DotenvFiles: []string{first, second}, Defaults: map[string]string{"default": "value"}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("SDK_ENV_UNCHANGED", "") != "first" || client.GetDefault("expanded", "") != "hello world" || client.GetDefault("empty", "fallback") != "" {
		t.Fatal("dotenv parsing or file priority incorrect")
	}
	if os.Getenv("SDK_ENV_UNCHANGED") != "process" {
		t.Fatal("SDK mutated process environment")
	}
	if err := os.WriteFile(first, []byte("BASE=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("BASE", "") != "changed" || client.GetDefault("SDK_ENV_UNCHANGED", "") != "second" {
		t.Fatal("dotenv changes not reloaded")
	}
	if _, ok := client.Get("expanded"); ok {
		t.Fatal("deleted dotenv value remained in memory")
	}
	secret := "sensitive-content"
	if err := os.WriteFile(first, []byte("!invalid="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	err := client.Load(t.Context())
	if !errors.Is(err, ErrInvalidDotenv) || strings.Contains(err.Error(), secret) {
		t.Fatal("dotenv error missing or exposed contents", err)
	}
	if client.GetDefault("other", "") != "value" || client.GetDefault("default", "") != "value" {
		t.Fatal("dotenv failure blocked lower sources")
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := client.Load(t.Context()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("explicit missing dotenv file was ignored", err)
	}
}

func TestOptionalAndDisabledDotenv(t *testing.T) {
	dir := filepath.Dir(scratchTestPath(t))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	client := testClient(t, Config{})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal("missing optional .env", err)
	}
	if err := os.WriteFile(".env", []byte("key=dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "dotenv" {
		t.Fatal("default .env not read", err)
	}
	disabled := testClient(t, Config{DotenvFiles: []string{}})
	if err := disabled.Load(t.Context()); err != nil || len(disabled.All()) != 0 {
		t.Fatal("empty file list did not disable dotenv", err)
	}
}

func TestCancellationPreservesSnapshot(t *testing.T) {
	client := testClient(t, Config{DotenvFiles: []string{}, Defaults: map[string]string{"key": "default"}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Put(t.Context(), "key", "memory"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.LoadPrefix(ctx, "other."); !errors.Is(err, context.Canceled) || client.GetDefault("key", "") != "memory" {
		t.Fatal("cancelled load replaced snapshot", err)
	}
}

func TestRemotePutCachesOnlySuccessfulWrites(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/project"):
			json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "project"}})
		case r.Method == http.MethodPut:
			if fail.Load() {
				w.WriteHeader(http.StatusForbidden)
			}
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}}
	client := testClient(t, cfg)
	if err := client.Put(t.Context(), "key", "written"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if err := client.Put(t.Context(), "key", "rejected"); err == nil {
		t.Fatal("failed write accepted")
	}
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "written" {
		t.Fatal("cache did not preserve successful write", err)
	}
}

type failingSource struct {
	err error
}

func (s failingSource) Read(context.Context, string) ([]Item, error) {
	return nil, s.err
}

func TestDataSourceComposition(t *testing.T) {
	failure := errors.New("source unavailable")
	var source DataSource = Sources{
		MapSource{"db.empty": "", "other": "ignored"},
		failingSource{err: failure},
		MapSource{"db.empty": "default", "db.pool": "10"},
	}
	items, err := source.Read(t.Context(), "db.")
	if !errors.Is(err, failure) || !reflect.DeepEqual(snapshotValues(items), map[string]string{"db.empty": "", "db.pool": "10"}) {
		t.Fatal("composed source lost fallback or failure", items, err)
	}
}

func TestConcurrentRemoteLoadsAndWrites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/project"):
			json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "project"}})
		case r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "remote", Value: "value"}}})
		}
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}})
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if err := client.Load(t.Context()); err != nil {
				t.Error(err)
			}
			if err := client.Put(t.Context(), "written", "value"); err != nil {
				t.Error(err)
			}
			client.All()
			client.Get("written")
		})
	}
	wg.Wait()
	if err := client.Load(t.Context()); err != nil || len(client.All()) != 2 {
		t.Fatal("concurrent operations lost cached writes", err)
	}
}
