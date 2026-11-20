package configcenter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func scratchTestPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".tmp", "sdk-dotenv-"+rand.Text()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return filepath.Join(root, "settings.env")
}

func testClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func expireCache(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()
	for key, entry := range client.cache.entries {
		entry.expiresAt = time.Now().Add(-time.Second)
		client.cache.entries[key] = entry
	}
	for prefix, scope := range client.cache.scopes {
		scope.expiresAt = time.Now().Add(-time.Second)
		client.cache.scopes[prefix] = scope
	}
}

func TestMemoryTTLDoesNotSlideOnReads(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Minute} {
		t.Run(ttl.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := testClient(t, Config{CacheTTL: ttl, DotenvFiles: []string{}, Defaults: map[string]string{"key": "default"}})
				if ttl == 0 {
					ttl = DefaultCacheTTL
				}
				if err := client.Put(t.Context(), "key", "memory"); err != nil {
					t.Fatal(err)
				}
				if err := client.Put(t.Context(), "only_memory", "value"); err != nil {
					t.Fatal(err)
				}
				if !client.IsLocal() {
					t.Fatal("unconfigured SDK selected a remote backend")
				}
				time.Sleep(ttl - time.Nanosecond)
				if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "memory" {
					t.Fatal("cache expired early", err)
				}
				time.Sleep(time.Nanosecond)
				if client.GetDefault("key", "") != "memory" || len(client.All()) != 2 {
					t.Fatal("local writes expired with the remote cache TTL")
				}
				if _, ok := client.Get("only_memory"); !ok {
					t.Fatal("durable local value vanished")
				}
			})
		})
	}
	if _, err := New(Config{CacheTTL: -time.Second}); err == nil {
		t.Fatal("negative TTL accepted")
	}
}

func TestFallbackPriorityAndEmptyValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []Item{
			{Key: "memory", Value: "remote"},
			{Key: "memory_empty", Value: "remote"},
			{Key: "remote", Value: "remote-value", Description: "remote metadata"},
			{Key: "remote_empty", Value: ""},
			{Key: "invalid_int", Value: "invalid"},
		}})
	}))
	defer server.Close()
	file := dotenvTestFile(t, "memory=dotenv\nmemory_empty=dotenv\nremote=dotenv\nremote_empty=dotenv\ndotenv='dotenv value'\ndotenv_empty=\ninvalid_int=10\n")
	defaults := map[string]string{"memory": "default", "remote": "default", "dotenv": "default", "dotenv_empty": "default", "default": "default-value", "invalid_int": "20"}
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{file}, Defaults: defaults})
	client.cachePut(t.Context(), Item{Key: "memory", Value: "memory-value"})
	client.cachePut(t.Context(), Item{Key: "memory_empty", Value: ""})
	defaults["default"] = "mutated"
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"memory": "memory-value", "memory_empty": "", "remote": "remote-value", "remote_empty": "", "dotenv": "dotenv value", "dotenv_empty": "", "default": "default-value", "invalid_int": "invalid"}
	if got := snapshotValues(client.All()); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %#v, want %#v", got, want)
	}
	if client.GetDefault("missing", "caller") != "caller" || client.Int("invalid_int", 7) != 7 {
		t.Fatal("caller defaults not respected")
	}
	for _, item := range client.All() {
		if item.Key == "remote" && item.Description != "remote metadata" {
			t.Fatal("remote metadata lost")
		}
	}
	expireCache(client)
	if client.GetDefault("remote", "") != "dotenv" || client.GetDefault("default", "") != "default-value" {
		t.Fatal("expired getters did not use loaded fallbacks")
	}
}

func TestRemoteCacheHitsExpiryDeletionAndFailures(t *testing.T) {
	var mode, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		switch mode.Load() {
		case 1:
			json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "key", Value: "updated"}}})
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 3:
			json.NewEncoder(w).Encode(map[string]any{"items": []Item{}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "key", Value: "initial"}, {Key: "deleted", Value: "old"}}})
		}
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}, Defaults: map[string]string{"key": "default"}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	if err := client.Load(t.Context()); err != nil || reads.Load() != 1 || client.GetDefault("key", "") != "initial" {
		t.Fatal("cache hit contacted remote or changed the value", err)
	}
	expireCache(client)
	if err := client.Load(t.Context()); err != nil || reads.Load() != 2 || client.GetDefault("key", "") != "updated" {
		t.Fatal("expiry did not refresh the remote value", err)
	}
	if _, ok := client.Get("deleted"); ok {
		t.Fatal("remote deletion kept an expired value")
	}
	mode.Store(2)
	if err := client.Load(t.Context()); err != nil || reads.Load() != 2 {
		t.Fatal("fresh cache did not bypass unavailable remote", err)
	}
	expireCache(client)
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "default" {
		t.Fatal("remote failure reused expired data", err)
	}
	mode.Store(3)
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	count := reads.Load()
	if err := client.Load(t.Context()); err != nil || reads.Load() != count || client.GetDefault("key", "") != "default" {
		t.Fatal("empty remote snapshot was not cached", err)
	}
	expireCache(client)
	mode.Store(1)
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "updated" {
		t.Fatal("negative cache did not expire", err)
	}
	server.Close()
	expireCache(client)
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "default" {
		t.Fatal("transport failure reused expired data", err)
	}
}

func TestPrefixCacheCoverage(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		items := []Item{{Key: "db.pool", Value: "20"}, {Key: "feature.enabled", Value: "true"}}
		if r.URL.Query().Get("prefix") != "" {
			items = items[:1]
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}})
	if err := client.LoadPrefix(t.Context(), "db."); err != nil || reads.Load() != 1 || len(client.All()) != 1 {
		t.Fatal("prefix load failed", err)
	}
	if err := client.LoadPrefix(t.Context(), "db.pool"); err != nil || reads.Load() != 1 {
		t.Fatal("narrower prefix missed cache", err)
	}
	if err := client.Load(t.Context()); err != nil || reads.Load() != 2 || len(client.All()) != 2 {
		t.Fatal("partial cache prevented full fetch", err)
	}
	if err := client.LoadPrefix(t.Context(), "feature."); err != nil || reads.Load() != 2 || len(client.All()) != 1 {
		t.Fatal("full cache did not cover prefix", err)
	}
	if _, ok := client.Get("db.pool"); ok {
		t.Fatal("getter ignored loaded prefix")
	}
	client.mu.Lock()
	deadline := client.cache.entries["db.pool"].expiresAt
	if client.cache.scopes[""].expiresAt.After(deadline) {
		t.Error("broader fetch extended earlier cache TTL")
	}
	client.mu.Unlock()
	expireCache(client)
	if err := client.LoadPrefix(t.Context(), "db."); err != nil || reads.Load() != 3 {
		t.Fatal("expired prefix did not refresh", err)
	}
	if err := client.Load(t.Context()); err != nil || reads.Load() != 4 || len(client.All()) != 2 {
		t.Fatal("prefix refresh incorrectly marked entire cache fresh", err)
	}
}

func TestCacheIsIsolatedAcrossClients(t *testing.T) {
	client := testClient(t, Config{DotenvFiles: []string{}})
	if err := client.Put(t.Context(), "key", "private"); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"production", "staging"} {
		other := testClient(t, Config{Environment: environment, DotenvFiles: []string{}})
		if err := other.Load(t.Context()); err != nil || len(other.All()) != 0 {
			t.Fatal("memory was shared across clients", err)
		}
	}
}

func TestMemoryConcurrentWritesAndCancellation(t *testing.T) {
	client := testClient(t, Config{DotenvFiles: []string{}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.Put(ctx, "key", "value"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled write", err)
	}
	if err := client.EnsureDefaults(ctx, map[string]string{"key": "value"}); !errors.Is(err, context.Canceled) || len(client.All()) != 0 {
		t.Fatal("cancelled initialization mutated memory", err)
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			if err := client.Put(t.Context(), fmt.Sprintf("key.%02d", i), "value"); err != nil {
				t.Error(err)
			}
			client.Get("key.00")
			client.All()
		})
	}
	wg.Wait()
	if err := client.LoadPrefix(t.Context(), "key.0"); err != nil || len(client.All()) != 10 {
		t.Fatal("prefix snapshot contains unrelated writes", err)
	}
	if err := client.Load(t.Context()); err != nil || len(client.All()) != 32 {
		t.Fatal("lost concurrent writes", err)
	}
}

func TestRemoteErrorsDoNotRedirectWrites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}, Defaults: map[string]string{"key": "default"}})
	if client.IsLocal() {
		t.Fatal("configured client selected local backend")
	}
	if err := client.Load(t.Context()); err != nil {
		t.Fatal("remote read failure blocked fallback loading", err)
	}
	if err := client.Put(t.Context(), "key", "value"); err == nil || client.GetDefault("key", "") != "default" {
		t.Fatal("remote write redirected to memory", err)
	}
}

func TestWatchRefreshesExpiredRemoteCache(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "version", Value: fmt.Sprint(version.Load())}}})
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}})
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		client.opMu.Lock()
		defer client.opMu.Unlock()
	}()
	if err := client.Watch(ctx, WatchOptions{Interval: 5 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	version.Store(2)
	expireCache(client)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.Int("version", 0) == 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("watcher did not refresh expired cache")
}
