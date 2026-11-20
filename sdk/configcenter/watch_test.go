package configcenter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// revisionServer emulates the conditional endpoint of the server: it returns
// the full payload once, then 304 while the revision matches.
type revisionServer struct {
	mu          sync.Mutex
	values      map[string]string
	conditional int32
	full        int32
}

func (s *revisionServer) set(values map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = values
}

func (s *revisionServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, etag := s.payload(t)
		if match := r.Header.Get("If-None-Match"); match != "" {
			atomic.AddInt32(&s.conditional, 1)
			if match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		} else {
			atomic.AddInt32(&s.full, 1)
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write(payload)
	}
}

func (s *revisionServer) payload(t *testing.T) (payload []byte, etag string) {
	t.Helper()
	s.mu.Lock()
	items := make([]Item, 0, len(s.values))
	for key, value := range s.values {
		items = append(items, Item{Key: key, Value: value})
	}
	s.mu.Unlock()
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Error(err)
		return nil, ""
	}
	sum := sha256.Sum256(payload)
	return payload, `"` + hex.EncodeToString(sum[:]) + `"`
}

// rewrite stores the payload of another client into the persisted file, which
// simulates a second process writing its snapshot.
func (s *revisionServer) rewrite(t *testing.T, path, environment string, fetchedAt time.Time) {
	t.Helper()
	payload, etag := s.payload(t)
	var body struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	snapshot := fileSnapshot{
		Version: fileCacheVersion, Environment: environment, Prefix: "",
		Revision: etag, FetchedAt: fetchedAt.UTC(), Items: body.Items,
	}
	encoded, err := encodeFileSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileSnapshot(path, encoded); err != nil {
		t.Fatal(err)
	}
}

func cacheFilePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(filepath.Dir(scratchTestPath(t)), "snapshot.json")
}

func TestWatchOnlyReportsChangedSnapshots(t *testing.T) {
	server := &revisionServer{values: map[string]string{"key": "initial"}}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()
	client := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheTTL: 5 * time.Millisecond})
	var changes atomic.Int32
	var last atomic.Value
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		client.opMu.Lock()
		defer client.opMu.Unlock()
	}()
	if err := client.Watch(ctx, WatchOptions{
		Interval: 2 * time.Millisecond,
		OnChange: func(items []Item) {
			changes.Add(1)
			last.Store(snapshotValues(items))
		},
	}); err != nil {
		t.Fatal(err)
	}
	if changes.Load() != 1 || !reflect.DeepEqual(last.Load(), map[string]string{"key": "initial"}) {
		t.Fatal("initial load did not notify exactly once", changes.Load())
	}
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&server.conditional) < 3 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&server.conditional) < 3 {
		t.Fatal("watcher stopped revalidating")
	}
	if changes.Load() != 1 {
		t.Fatal("unchanged polls notified a change", changes.Load())
	}
	server.set(map[string]string{"key": "updated"})
	for time.Now().Before(deadline) {
		if changes.Load() == 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if changes.Load() != 2 || !reflect.DeepEqual(last.Load(), map[string]string{"key": "updated"}) {
		t.Fatal("changed snapshot was not reported", changes.Load(), last.Load())
	}
	server.set(map[string]string{"key": "updated"})
	settled := atomic.LoadInt32(&server.conditional)
	time.Sleep(20 * time.Millisecond)
	if changes.Load() != 2 || atomic.LoadInt32(&server.conditional) <= settled {
		t.Fatal("identical content after a change notified again", changes.Load())
	}
}

func TestConditionalRevalidationKeepsExpiredValues(t *testing.T) {
	server := &revisionServer{values: map[string]string{"key": "cached"}}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()
	client := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	expireCache(client)
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&server.conditional); got != 1 {
		t.Fatal("expired snapshot was not revalidated", got)
	}
	if client.GetDefault("key", "") != "cached" || atomic.LoadInt32(&server.full) != 1 {
		t.Fatal("304 did not keep the renewed snapshot", server.full)
	}
	expireCache(client)
	server.set(map[string]string{"key": "changed", "other": "value"})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.GetDefault("key", "") != "changed" || client.GetDefault("other", "") != "value" {
		t.Fatal("changed revision was not downloaded")
	}
	if got := atomic.LoadInt32(&server.conditional); got != 2 {
		t.Fatal("stale revision was not revalidated", got)
	}
}

func TestLoadRenewsIdenticalPayloadWithoutPublishing(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "key", Value: "same"}}})
	}))
	defer srv.Close()
	client := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	client.opMu.Lock()
	changed, remoteErr, localErr := client.reloadLocked(t.Context(), "", true)
	client.opMu.Unlock()
	if changed || remoteErr != nil || localErr != nil {
		t.Fatal("identical refresh reported a change", changed, remoteErr, localErr)
	}
	if calls.Load() != 2 {
		t.Fatal("forced refresh skipped the request", calls.Load())
	}
}

func TestFileCacheWarmsUpAndRevalidates(t *testing.T) {
	path := cacheFilePath(t)
	server := &revisionServer{values: map[string]string{"db.pool": "10"}}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()
	first := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if err := first.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("snapshot was not persisted", err)
	}
	warm := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if warm.GetDefault("db.pool", "") != "10" || warm.Int("db.pool", 0) != 10 {
		t.Fatal("warm start did not restore the snapshot")
	}
	if err := warm.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&server.full) + atomic.LoadInt32(&server.conditional); got != 1 {
		t.Fatal("a fresh persisted snapshot still contacted the server", got)
	}
	server.rewrite(t, path, "production", time.Now().Add(-time.Hour))
	stale := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if _, ok := stale.Get("db.pool"); ok {
		t.Fatal("expired persisted snapshot stayed visible")
	}
	if err := stale.Load(t.Context()); err != nil || stale.GetDefault("db.pool", "") != "10" {
		t.Fatal("expired cache was not restored after revalidation", err)
	}
	if got := atomic.LoadInt32(&server.conditional); got != 1 {
		t.Fatal("expired snapshot was not revalidated", got)
	}
	if atomic.LoadInt32(&server.full) != 1 {
		t.Fatal("unchanged revalidation downloaded the payload")
	}
	server.rewrite(t, path, "production", time.Now().Add(-time.Hour))
	server.set(map[string]string{"db.pool": "20"})
	changed := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if err := changed.Load(t.Context()); err != nil || changed.GetDefault("db.pool", "") != "20" {
		t.Fatal("expired cache did not refresh", err)
	}
}

func TestFileCacheIsScopedAndReported(t *testing.T) {
	path := cacheFilePath(t)
	server := &revisionServer{values: map[string]string{"key": "production"}}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()
	client := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	other := testClient(t, Config{Environment: "staging", BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if _, ok := other.Get("key"); ok {
		t.Fatal("snapshot leaked across environments")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path})
	if _, ok := broken.Get("key"); ok {
		t.Fatal("corrupt snapshot was used")
	}
	if err := broken.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted fileSnapshot
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.Version != fileCacheVersion || persisted.Environment != "production" {
		t.Fatal("snapshot was not rewritten", err)
	}
	absolute := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: filepath.Join(filepath.Dir(path), "absolute.json")})
	if err := absolute.Load(t.Context()); err != nil {
		t.Fatal("absolute cache path was rejected", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "absolute.json")); err != nil {
		t.Fatal("absolute cache path was not written", err)
	}
}

func TestFileCacheIsolatedAcrossClientsWithLocalWrites(t *testing.T) {
	path := cacheFilePath(t)
	values := map[string]string{"remote": "value"}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/project") {
			_ = json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "app"}})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		items := make([]Item, 0, len(values))
		for key, value := range values {
			items = append(items, Item{Key: key, Value: value})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer srv.Close()
	client := testClient(t, Config{BaseURL: srv.URL, APIKey: "cc_test", DotenvFiles: []string{}, CacheFile: path, Defaults: map[string]string{"local": "default"}})
	if err := client.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Put(t.Context(), "remote", "written"); err != nil {
		t.Fatal(err)
	}
	persisted := loadPersisted(t, path)
	if !reflect.DeepEqual(snapshotValues(persisted.Items), map[string]string{"remote": "written"}) {
		t.Fatal("local write did not refresh the file cache", persisted.Items)
	}
	if persisted.Revision != "" {
		t.Fatal("a local write kept a stale revision")
	}
	mu.Lock()
	values["remote"] = "written"
	mu.Unlock()
	if err := client.Load(t.Context()); err != nil || client.GetDefault("remote", "") != "written" {
		t.Fatal("unconditional read after a local write failed", err)
	}
}

func loadPersisted(t *testing.T, path string) fileSnapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted fileSnapshot
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	return persisted
}
