// Package configcenter reads and writes remote or local configuration.
//
// The client authenticates with an API key issued for one project. It carries
// no project name: the key itself is bound to a project, so a key can never
// read another project's configuration. Without a server URL and API key,
// the client uses memory, dotenv files and application defaults.
package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound is returned when the server has no such project or environment.
	ErrNotFound = errors.New("configcenter: not found")
	// ErrKeyNotFound is returned when a key is absent from a loaded snapshot.
	ErrKeyNotFound = errors.New("configcenter: key not found")
)

// Config selects the memory cache, file cache, remote, dotenv and default sources.
type Config struct {
	// CacheTTL bounds the lifetime of cached reads and writes without extending on hits.
	// Zero uses DefaultCacheTTL; negative durations are invalid.
	CacheTTL time.Duration
	// CacheFile persists the last remote snapshot for warm starts; empty disables it.
	CacheFile string
	// DotenvFiles lists dotenv files in priority order. Nil reads an optional .env;
	// an empty slice disables dotenv. Explicit files must exist.
	DotenvFiles []string
	// Defaults supplies values below memory, remote and dotenv values.
	Defaults map[string]string
	// BaseURL is the server root, for example http://127.0.0.1:8080.
	BaseURL string
	// APIKey is the token created on the project's API keys page.
	APIKey string
	// RevealSecrets opts into the privileged read path requiring config:read-secrets.
	RevealSecrets bool
	// Environment is the default environment slug; empty means production.
	Environment string
	// HTTPClient overrides the default client.
	HTTPClient *http.Client
	// Timeout overrides the default request timeout of 30 seconds.
	Timeout time.Duration
	// OnError reports nonfatal remote failures from Load and LoadPrefix after
	// publishing fallback values and releasing locks. Nil logs a warning.
	OnError func(error)
}

// Item is one configuration entry.
type Item struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Secret      bool      `json:"secret"`
	Masked      bool      `json:"masked"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WatchOptions controls the polling loop.
type WatchOptions struct {
	Interval time.Duration
	// OnChange receives the published snapshot only after a load changed it.
	// It runs without internal locks, so it may read the client.
	OnChange func([]Item)
	// OnError reports initial and subsequent source failures. It overrides
	// Config.OnError for this watcher; nil uses that handler or logs a warning.
	OnError func(error)
}

// Client accesses a remote project with an isolated, expiring memory cache.
type Client struct {
	sources       Sources
	endpoint      string
	apiKey        string
	cacheFile     string
	revealSecrets bool
	env           string
	http          *http.Client
	onError       func(error)

	opMu      sync.Mutex
	mu        sync.RWMutex
	cache     memoryCache
	fallbacks Snapshot
	prefix    string
	persisted string
}

// New validates cfg and returns a client.
func New(cfg Config) (*Client, error) {
	if cfg.CacheTTL < 0 {
		return nil, errors.New("configcenter: CacheTTL must not be negative")
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}
	env := cfg.Environment
	if env == "" {
		env = "production"
	}
	if cfg.BaseURL == "" && cfg.APIKey == "" {
		return newClient(cfg, env, "", nil), nil
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("configcenter: BaseURL is required")
	}
	if !strings.HasPrefix(cfg.APIKey, "cc_") {
		return nil, errors.New("configcenter: a cc_ API key is required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("configcenter: BaseURL must be an HTTP(S) URL without credentials, query or fragment")
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return newClient(cfg, env, base.String()+"/api/v1/agent", client), nil
}

func newClient(cfg Config, env, endpoint string, client *http.Client) *Client {
	c := &Client{
		endpoint:      endpoint,
		apiKey:        cfg.APIKey,
		cacheFile:     cfg.CacheFile,
		revealSecrets: cfg.RevealSecrets,
		env:           env,
		http:          client,
		onError:       cfg.OnError,
		cache:         newMemoryCache(cfg.CacheTTL),
	}
	var files []string
	if cfg.DotenvFiles != nil {
		files = append([]string{}, cfg.DotenvFiles...)
	}
	c.sources = Sources{DotenvSource{Files: files}, MapSource(maps.Clone(cfg.Defaults))}
	c.warmStart()
	return c
}

// IsLocal reports whether no remote server is configured; writes then use memory with TTL.
func (c *Client) IsLocal() bool { return c.endpoint == "" }

// Environment returns the environment slug the client reads.
func (c *Client) Environment() string {
	return c.env
}

func (c *Client) valuesPath() string {
	path := "/environments/" + url.PathEscape(c.env) + "/values"
	if c.revealSecrets {
		path += "/reveal"
	}
	return path
}

// Load merges all sources by priority and replaces the snapshot.
// Remote failures are reported through Config.OnError or a warning; local
// source errors and context cancellation are returned to the caller.
func (c *Client) Load(ctx context.Context) error {
	return c.load(ctx, "", c.onError)
}

// LoadPrefix loads matching keys with the same fallback behavior as Load.
func (c *Client) LoadPrefix(ctx context.Context, prefix string) error {
	return c.load(ctx, prefix, c.onError)
}

func (c *Client) load(ctx context.Context, prefix string, onError func(error)) error {
	_, remoteErr, localErr := c.reload(ctx, prefix, false)
	if localErr != nil {
		return errors.Join(remoteErr, localErr)
	}
	if remoteErr != nil {
		reportSourceError(ctx, remoteErr, onError)
	}
	return nil
}

// reload refreshes the snapshot and reports whether the published values
// changed. A fetched payload identical to the cache renews the TTL instead of
// replacing it, so unchanged polls never publish or notify.
func (c *Client) reload(ctx context.Context, prefix string, refresh bool) (bool, error, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.reloadLocked(ctx, prefix, refresh)
}

// reloadLocked is reload without taking the operation lock; callers that
// already hold it keep writes and reads ordered.
func (c *Client) reloadLocked(ctx context.Context, prefix string, refresh bool) (bool, error, error) {
	if err := ctx.Err(); err != nil {
		return false, nil, fmt.Errorf("configcenter: load cancelled: %w", err)
	}
	now := time.Now()
	c.mu.Lock()
	c.cache.prune(now)
	covered := c.cache.covers(prefix, now)
	revision, revalidate := c.cache.revision(prefix)
	before := revisionOf(c.configurationLocked(prefix))
	c.mu.Unlock()

	fetch := !c.IsLocal() && (refresh || !covered)
	var remoteErr error
	var items []Item
	notModified := false
	if fetch {
		validator := ""
		if revalidate {
			validator = revision
		}
		items, revision, notModified, remoteErr = c.fetchRemote(ctx, prefix, validator)
	}
	fetchedAt := time.Now()
	fallbacks, fallbackErr := LoadSnapshot(ctx, prefix, c.sources...)
	if err := ctx.Err(); err != nil {
		return false, nil, fmt.Errorf("configcenter: load cancelled: %w", err)
	}

	c.mu.Lock()
	persist := fetch && remoteErr == nil && !notModified
	if fetch && remoteErr == nil {
		if notModified {
			c.cache.renew(prefix, fetchedAt)
		} else {
			c.cache.store(prefix, items, fetchedAt, refresh, revision)
		}
	}
	c.fallbacks = fallbacks
	c.prefix = prefix
	changed := before != revisionOf(c.configurationLocked(prefix))
	var snapshot fileSnapshot
	if persist {
		snapshot = c.fileSnapshotLocked(prefix, fetchedAt)
	}
	c.mu.Unlock()

	if persist {
		c.persistSnapshot(snapshot)
	}
	return changed, remoteErr, fallbackErr
}

// configurationLocked merges commit metadata with the retained payload. It
// ignores TTL so expiry itself never counts as a configuration change.
func (c *Client) configurationLocked(prefix string) []Item {
	values := make(map[string]Item, len(c.fallbacks.items))
	maps.Copy(values, c.fallbacks.items)
	for _, item := range c.cache.all(prefix) {
		values[item.Key] = item
	}
	out := make([]Item, 0, len(values))
	for _, item := range values {
		out = append(out, item)
	}
	return out
}

// effectiveLocked is what reads currently observe.
func (c *Client) effectiveLocked(prefix string, now time.Time) []Item {
	values := make(map[string]Item, len(c.fallbacks.items))
	maps.Copy(values, c.fallbacks.items)
	for _, item := range c.cache.items(prefix, now) {
		values[item.Key] = item
	}
	out := make([]Item, 0, len(values))
	for _, item := range values {
		out = append(out, item)
	}
	return out
}

func (c *Client) publishedLocked(now time.Time) []Item {
	return c.effectiveLocked(c.prefix, now)
}

func (c *Client) fetchRemote(ctx context.Context, prefix, validator string) (items []Item, revision string, notModified bool, err error) {
	query := url.Values{}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	var body struct {
		Items []Item `json:"items"`
	}
	notModified, serverRevision, err := c.conditionalGet(ctx, c.valuesPath(), query, validator, &body)
	if err != nil || notModified {
		return nil, validator, notModified, err
	}
	items = make([]Item, 0, len(body.Items))
	for _, item := range body.Items {
		if err := validateKey(item.Key); err != nil {
			return nil, "", false, fmt.Errorf("configcenter: invalid remote key: %w", err)
		}
		if strings.HasPrefix(item.Key, prefix) {
			items = append(items, item)
		}
	}
	if serverRevision == "" {
		serverRevision = revisionOf(items, prefix)
	}
	return items, serverRevision, false, nil
}

// conditionalGet performs a GET that reports an unchanged revision instead of
// failing. It returns the response validator when the server sent one.
func (c *Client) conditionalGet(ctx context.Context, path string, query url.Values, validator string, out any) (notModified bool, revision string, err error) {
	req, err := c.request(ctx, http.MethodGet, c.endpoint+path, http.NoBody, query)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Accept", "application/json")
	if validator != "" {
		req.Header.Set("If-None-Match", validator)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("configcenter: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // a close error cannot be reported after the body was read
	switch resp.StatusCode {
	case http.StatusNotModified:
		if validator == "" {
			return false, "", fmt.Errorf("configcenter: %s: unexpected 304", path)
		}
		return true, validator, nil
	case http.StatusNotFound:
		return false, "", fmt.Errorf("configcenter: %s: %w", path, ErrNotFound)
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return false, "", fmt.Errorf("configcenter: decode %s: %w", path, err)
		}
		return false, resp.Header.Get("ETag"), nil
	default:
		return false, "", fmt.Errorf("configcenter: %s: status %d", path, resp.StatusCode)
	}
}

func reportSourceError(ctx context.Context, err error, onError func(error)) {
	if onError != nil {
		onError(err)
		return
	}
	slog.WarnContext(ctx, "configcenter configuration source unavailable", "error", err)
}

func (c *Client) request(ctx context.Context, method, target string, body io.Reader, query url.Values) (*http.Request, error) {
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("configcenter: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	req, err := c.request(ctx, http.MethodGet, c.endpoint+path, http.NoBody, query)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("configcenter: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // a close error cannot be reported after the body was read
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("configcenter: %s: %w", path, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		// The status code is the useful error; the body text is not needed. //nolint:errcheck
		return fmt.Errorf("configcenter: %s: status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("configcenter: decode %s: %w", path, err)
	}
	return nil
}

// Get returns an unexpired cached value or the loaded fallback without network I/O.
func (c *Client) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !strings.HasPrefix(key, c.prefix) {
		return "", false
	}
	if item, ok := c.cache.get(key, time.Now()); ok {
		return item.Value, true
	}
	return c.fallbacks.Get(key)
}

// GetDefault returns the snapshot value of key or fallback.
func (c *Client) GetDefault(key, fallback string) string {
	if value, ok := c.Get(key); ok {
		return value
	}
	return fallback
}

// Int parses the snapshot value of key as an integer, falling back on error.
func (c *Client) Int(key string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.GetDefault(key, "")))
	if err != nil {
		return fallback
	}
	return n
}

// Bool parses the snapshot value of key as a boolean, falling back on error.
func (c *Client) Bool(key string, fallback bool) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(c.GetDefault(key, "")))
	if err != nil {
		return fallback
	}
	return b
}

// All returns unexpired cached values and loaded fallbacks sorted by key.
func (c *Client) All() []Item {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := c.publishedLocked(time.Now())
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

// Watch reloads until ctx is cancelled. The first load happens before Watch
// returns control. Remote failures report an error and use local fallbacks
// without preventing startup or subsequent polling. Polling respects
// CacheTTL and revalidates with If-None-Match, so an unchanged environment
// costs one empty response per expiry. OnChange runs only after a load
// changed the published values.
func (c *Client) Watch(ctx context.Context, opts WatchOptions) error {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	onError := opts.OnError
	if onError == nil {
		onError = c.onError
	}
	changed, remoteErr, localErr := c.reload(ctx, "", false)
	if localErr != nil {
		return errors.Join(remoteErr, localErr)
	}
	if remoteErr != nil {
		reportSourceError(ctx, remoteErr, onError)
	}
	if changed && opts.OnChange != nil {
		opts.OnChange(c.All())
	}
	go func() {
		ticker := time.NewTicker(opts.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed, remoteErr, localErr := c.reload(ctx, "", false)
				if ctx.Err() != nil {
					return
				}
				if err := errors.Join(remoteErr, localErr); err != nil {
					reportSourceError(ctx, err, onError)
				}
				if changed && opts.OnChange != nil {
					opts.OnChange(c.All())
				}
			}
		}
	}()
	return nil
}

// warmStart restores the persisted snapshot, allowing reads before the first
// request. Expired entries stay invisible until a conditional request
// confirms the server still serves the same revision.
func (c *Client) warmStart() {
	if c.cacheFile == "" {
		return
	}
	persisted, err := readFileSnapshot(c.cacheFile)
	if err != nil {
		slog.Warn("configcenter local cache unavailable", "path", c.cacheFile, "error", err)
		return
	}
	if persisted.Environment != c.env {
		return
	}
	items := make([]Item, 0, len(persisted.Items))
	for _, item := range persisted.Items {
		if validateKey(item.Key) == nil && strings.HasPrefix(item.Key, persisted.Prefix) {
			items = append(items, item)
		}
	}
	c.cache.restore(persisted.Prefix, cacheScope{expiresAt: persisted.FetchedAt.Add(c.cache.ttl), revision: persisted.Revision}, items)
}

func (c *Client) fileSnapshotLocked(prefix string, now time.Time) fileSnapshot {
	snapshot := fileSnapshot{
		Version:     fileCacheVersion,
		Environment: c.env,
		Prefix:      prefix,
		FetchedAt:   now.UTC(),
		Items:       sortedItems(c.cache.items(prefix, now)),
	}
	if scope, ok := c.cache.scope(prefix); ok {
		snapshot.Revision = scope.revision
	}
	return snapshot
}

func (c *Client) persistSnapshot(snapshot fileSnapshot) {
	if c.cacheFile == "" || snapshot.Version == 0 {
		return
	}
	encoded, err := encodeFileSnapshot(snapshot)
	if err != nil {
		slog.Warn("configcenter local cache not written", "path", c.cacheFile, "error", err)
		return
	}
	digest := digestOf(encoded)
	c.mu.RLock()
	unchanged := digest == c.persisted
	c.mu.RUnlock()
	if unchanged {
		return
	}
	if err := writeFileSnapshot(c.cacheFile, encoded); err != nil {
		slog.Warn("configcenter local cache not written", "path", c.cacheFile, "error", err)
		return
	}
	c.mu.Lock()
	c.persisted = digest
	c.mu.Unlock()
}
