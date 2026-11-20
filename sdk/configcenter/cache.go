package configcenter

import (
	"strings"
	"time"
)

// DefaultCacheTTL limits how long a successful read or write stays in memory.
const DefaultCacheTTL = 30 * time.Second

type cacheEntry struct {
	item      Item
	expiresAt time.Time
}

// cacheScope records how long one loaded prefix stays valid and which server
// revision it was read as. The revision outlives the entries so an expired
// snapshot can be revalidated with one conditional request instead of a full
// download.
type cacheScope struct {
	expiresAt time.Time
	revision  string
}

type memoryCache struct {
	entries map[string]cacheEntry
	scopes  map[string]cacheScope
	ttl     time.Duration
}

func newMemoryCache(ttl time.Duration) memoryCache {
	return memoryCache{
		entries: make(map[string]cacheEntry),
		scopes:  make(map[string]cacheScope),
		ttl:     ttl,
	}
}

// prune drops expired entries unless a recorded revision still covers them.
// Those wait for a conditional read, which either resurrects the exact set the
// server confirmed or replaces it with the changed payload.
func (c *memoryCache) prune(now time.Time) {
	for key, entry := range c.entries {
		if now.Before(entry.expiresAt) || c.covered(key) {
			continue
		}
		delete(c.entries, key)
	}
	for prefix, scope := range c.scopes {
		if !now.Before(scope.expiresAt) && scope.revision == "" {
			delete(c.scopes, prefix)
		}
	}
}

func (c *memoryCache) covered(key string) bool {
	for prefix, scope := range c.scopes {
		if scope.revision != "" && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (c *memoryCache) covers(prefix string, now time.Time) bool {
	for cachedPrefix, scope := range c.scopes {
		if now.Before(scope.expiresAt) && strings.HasPrefix(prefix, cachedPrefix) {
			return true
		}
	}
	return false
}

// revision returns the recorded validator of prefix, ignoring broader scopes.
func (c *memoryCache) revision(prefix string) (string, bool) {
	scope, ok := c.scopes[prefix]
	if !ok || scope.revision == "" {
		return "", false
	}
	return scope.revision, true
}

func (c *memoryCache) scope(prefix string) (cacheScope, bool) {
	scope, ok := c.scopes[prefix]
	return scope, ok
}

func (c *memoryCache) get(key string, now time.Time) (Item, bool) {
	entry, ok := c.entries[key]
	return entry.item, ok && now.Before(entry.expiresAt)
}

func (c *memoryCache) put(item Item, now time.Time) {
	c.entries[item.Key] = cacheEntry{item: item, expiresAt: now.Add(c.ttl)}
}

// store records a fetched payload as the authoritative content of prefix.
// A forced store replaces every entry, while an incremental one keeps still
// valid entries written locally and drops entries the server no longer serves.
func (c *memoryCache) store(prefix string, items []Item, now time.Time, replace bool, revision string) {
	c.prune(now)
	present := make(map[string]bool, len(items))
	for _, item := range items {
		present[item.Key] = true
		if entry, ok := c.entries[item.Key]; ok && !replace && now.Before(entry.expiresAt) {
			continue
		}
		c.put(item, now)
	}
	for key, entry := range c.entries {
		if !strings.HasPrefix(key, prefix) || present[key] {
			continue
		}
		if replace || !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	expiresAt := now.Add(c.ttl)
	for key, entry := range c.entries {
		if strings.HasPrefix(key, prefix) && entry.expiresAt.Before(expiresAt) {
			expiresAt = entry.expiresAt
		}
	}
	for cachedPrefix := range c.scopes {
		if strings.HasPrefix(cachedPrefix, prefix) {
			delete(c.scopes, cachedPrefix)
		}
	}
	c.scopes[prefix] = cacheScope{expiresAt: expiresAt, revision: revision}
}

// renew extends prefix and its retained entries after the server confirmed
// that the recorded revision is still current, so expired values recover
// without a download.
func (c *memoryCache) renew(prefix string, now time.Time) {
	scope, ok := c.scopes[prefix]
	if !ok || scope.revision == "" {
		return
	}
	expiresAt := now.Add(c.ttl)
	for key, entry := range c.entries {
		if strings.HasPrefix(key, prefix) {
			entry.expiresAt = expiresAt
			c.entries[key] = entry
		}
	}
	scope.expiresAt = expiresAt
	c.scopes[prefix] = scope
}

// dropRevisions makes the next remote read of every scope unconditional.
func (c *memoryCache) dropRevisions() {
	for prefix, scope := range c.scopes {
		scope.revision = ""
		c.scopes[prefix] = scope
	}
}

// restore seeds an empty cache with a persisted snapshot. Entries keep the
// persisted expiry, so an expired file is invisible until a conditional
// request confirms its revision.
func (c *memoryCache) restore(prefix string, scope cacheScope, items []Item) {
	if _, exists := c.scopes[prefix]; exists || (len(items) == 0 && scope.revision == "") {
		return
	}
	for _, item := range items {
		c.entries[item.Key] = cacheEntry{item: item, expiresAt: scope.expiresAt}
	}
	c.scopes[prefix] = scope
}

// all returns every retained cached item matching prefix, including entries
// whose TTL expired but whose revision is still recorded.
func (c *memoryCache) all(prefix string) []Item {
	items := make([]Item, 0, len(c.entries))
	for key, entry := range c.entries {
		if strings.HasPrefix(key, prefix) {
			items = append(items, entry.item)
		}
	}
	return items
}

// items returns the unexpired cached items matching prefix.
func (c *memoryCache) items(prefix string, now time.Time) []Item {
	items := make([]Item, 0, len(c.entries))
	for key := range c.entries {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if item, ok := c.get(key, now); ok {
			items = append(items, item)
		}
	}
	return items
}
