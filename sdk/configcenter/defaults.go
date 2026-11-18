package configcenter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EnsureDefaults creates missing keys on the remote server or in memory with TTL.
// It preserves existing backend values and reloads the snapshot, bypassing cached reads.
// Remote initialization uses batch reads and writes; run bootstrap from one instance.
// It requires config:read, config:write, projects:read and environments:read permissions.
func (c *Client) EnsureDefaults(ctx context.Context, defaults map[string]string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("configcenter: initialize defaults cancelled: %w", err)
	}
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		if err := validateKey(key); err != nil {
			return fmt.Errorf("initialize default key %q: %w", key, err)
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	if err := c.ensureDefaults(ctx, defaults); err != nil {
		return fmt.Errorf("initialize default keys [%s]: %w", strings.Join(keys, ", "), err)
	}
	return nil
}

func (c *Client) ensureDefaults(ctx context.Context, defaults map[string]string) error {
	if c.IsLocal() {
		c.mu.Lock()
		now := time.Now()
		c.cache.prune(now)
		for key, value := range defaults {
			if _, exists := c.cache.get(key, now); !exists {
				c.cache.put(Item{Key: key, Value: value, UpdatedAt: now.UTC()}, now)
			}
		}
		c.mu.Unlock()
	} else {
		existing, err := c.readMany(ctx, batchKeys(defaults))
		if err != nil {
			return err
		}
		missing := make(map[string]string)
		for key, value := range defaults {
			if _, ok := existing[key]; !ok {
				missing[key] = value
			}
		}
		if len(missing) > 0 {
			if err := c.putMany(ctx, missing); err != nil {
				return err
			}
		}
	}
	remoteErr, localErr := c.loadSnapshot(ctx, "", !c.IsLocal())
	return errors.Join(remoteErr, localErr)
}
