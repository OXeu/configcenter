package configcenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

func batchKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func validateBatch(keys []string) error {
	if len(keys) == 0 || len(keys) > 500 {
		return fmt.Errorf("configcenter: batch must contain 1 to 500 keys")
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if err := validateKey(key); err != nil {
			return fmt.Errorf("key %q: %w", key, err)
		}
		if seen[key] {
			return fmt.Errorf("configcenter: duplicate key %q", key)
		}
		seen[key] = true
	}
	return nil
}

// ReadMany reads selected keys directly from the backend in one request.
// Missing keys are omitted; remote reads bypass cached and fallback values.
// It does not replace the loaded snapshot. RevealSecrets requires config:read-secrets;
// ordinary remote reads require config:read and return masked secrets.
func (c *Client) ReadMany(ctx context.Context, keys []string) (map[string]string, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	values, err := c.readMany(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("read configuration keys [%s]: %w", strings.Join(keys, ", "), err)
	}
	return values, nil
}

func (c *Client) readMany(ctx context.Context, keys []string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("batch read cancelled: %w", err)
	}
	if err := validateBatch(keys); err != nil {
		return nil, err
	}
	values := make(map[string]string, len(keys))
	if c.IsLocal() {
		c.mu.RLock()
		defer c.mu.RUnlock()
		now := time.Now()
		for _, key := range keys {
			if item, ok := c.cache.get(key, now); ok {
				values[key] = item.Value
			}
		}
		return values, nil
	}
	var payload struct {
		Items []Item `json:"items"`
	}
	if err := c.get(ctx, c.valuesPath(), url.Values{"keys": keys}, &payload); err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(keys))
	for _, key := range keys {
		selected[key] = true
	}
	for _, item := range payload.Items {
		if selected[item.Key] {
			values[item.Key] = item.Value
		}
	}
	return values, nil
}

// PutMany creates or updates all values in one atomic backend operation.
// Remote writes need projects:read, environments:read and config:write permissions.
func (c *Client) PutMany(ctx context.Context, values map[string]string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.putMany(ctx, values); err != nil {
		return fmt.Errorf("write configuration keys [%s]: %w", strings.Join(batchKeys(values), ", "), err)
	}
	return nil
}

func (c *Client) putMany(ctx context.Context, values map[string]string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("batch write cancelled: %w", err)
	}
	keys := batchKeys(values)
	if err := validateBatch(keys); err != nil {
		return err
	}
	items := make([]Item, 0, len(keys))
	for _, key := range keys {
		items = append(items, Item{Key: key, Value: values[key], UpdatedAt: time.Now().UTC()})
	}
	if c.IsLocal() {
		c.cachePutMany(items)
		return nil
	}
	path, err := c.environmentPath(ctx)
	if err != nil {
		return err
	}
	payload := make([]map[string]string, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]string{"key": item.Key, "value": item.Value})
	}
	body, err := json.Marshal(map[string]any{"items": payload})
	if err != nil {
		return fmt.Errorf("encode configuration batch: %w", err)
	}
	if len(body) > 1<<20 {
		return fmt.Errorf("configcenter: batch body exceeds 1 MiB")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, path+"/items", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build batch write request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send batch write request: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close batch write response: %w", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("configcenter: batch write status %d", resp.StatusCode)
	}
	c.cachePutMany(items)
	return nil
}

func (c *Client) cachePutMany(items []Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.cache.prune(now)
	for _, item := range items {
		c.cache.put(item, now)
	}
}
