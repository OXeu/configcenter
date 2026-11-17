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

// Config selects the memory cache, remote, dotenv and default configuration sources.
type Config struct {
	// CacheTTL bounds the lifetime of cached reads and writes without extending on hits.
	// Zero uses DefaultCacheTTL; negative durations are invalid.
	CacheTTL time.Duration
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
	// OnError is called for transient failures; nil means retry silently.
	OnError func(error)
}

// Client accesses a remote project with an isolated, expiring memory cache.
type Client struct {
	sources       Sources
	endpoint      string
	apiKey        string
	revealSecrets bool
	env           string
	http          *http.Client

	opMu      sync.Mutex
	mu        sync.RWMutex
	cache     memoryCache
	fallbacks map[string]Item
	prefix    string
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
		revealSecrets: cfg.RevealSecrets,
		env:           env,
		http:          client,
		cache: memoryCache{
			entries:  make(map[string]cacheEntry),
			prefixes: make(map[string]time.Time),
			ttl:      cfg.CacheTTL,
		},
		fallbacks: make(map[string]Item),
	}
	var files []string
	if cfg.DotenvFiles != nil {
		files = append([]string{}, cfg.DotenvFiles...)
	}
	c.sources = Sources{DotenvSource{Files: files}, MapSource(maps.Clone(cfg.Defaults))}
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
// Source errors are returned even when fallback values are available.
func (c *Client) Load(ctx context.Context) error {
	return c.load(ctx, "")
}

// LoadPrefix fetches the items whose keys start with prefix.
func (c *Client) LoadPrefix(ctx context.Context, prefix string) error {
	return c.load(ctx, prefix)
}

func (c *Client) load(ctx context.Context, prefix string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.loadSnapshot(ctx, prefix, false)
}

func (c *Client) loadSnapshot(ctx context.Context, prefix string, refresh bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("configcenter: load cancelled: %w", err)
	}
	c.mu.Lock()
	c.cache.prune(time.Now())
	covered := c.cache.covers(prefix)
	c.mu.Unlock()
	var remote []Item
	var remoteErr error
	fetch := !c.IsLocal() && (refresh || !covered)
	if fetch {
		remote, remoteErr = (remoteSource{client: c}).Read(ctx, prefix)
	}
	fetchedAt := time.Now()
	fallbacks, fallbackErr := c.sources.Read(ctx, prefix)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("configcenter: load cancelled: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fetch && remoteErr == nil {
		c.cache.store(prefix, remote, fetchedAt, refresh)
	}
	c.fallbacks = make(map[string]Item, len(fallbacks))
	for _, item := range fallbacks {
		c.fallbacks[item.Key] = item
	}
	c.prefix = prefix
	return errors.Join(remoteErr, fallbackErr)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	target := c.endpoint + path
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return fmt.Errorf("configcenter: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
	item, ok := c.fallbacks[key]
	return item.Value, ok
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
	values := maps.Clone(c.fallbacks)
	now := time.Now()
	for key := range c.cache.entries {
		if item, ok := c.cache.get(key, now); ok && strings.HasPrefix(key, c.prefix) {
			values[key] = item
		}
	}
	out := make([]Item, 0, len(values))
	for _, item := range values {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Watch reloads on interval until ctx is cancelled. The first load happens
// before Watch returns control, so callers can use the snapshot right away.
func (c *Client) Watch(ctx context.Context, opts WatchOptions) error {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if err := c.Load(ctx); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(opts.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Load(ctx); err != nil && opts.OnError != nil {
					opts.OnError(err)
				}
			}
		}
	}()
	return nil
}
