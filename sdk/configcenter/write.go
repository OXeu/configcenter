package configcenter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,190}$`)

func validateKey(key string) error {
	if !keyPattern.MatchString(key) {
		return errors.New("configcenter: key must be 1-191 letters, digits or . _ : / - and start with a letter or digit")
	}
	return nil
}

// Put writes to the remote server when configured, then caches the value with TTL.
// Without a remote server it writes only to this client's memory cache.
// Remote writes need projects:read, environments:read and config:write permissions.
func (c *Client) Put(ctx context.Context, key, value string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.put(ctx, key, value); err != nil {
		return fmt.Errorf("write configuration key %q: %w", key, err)
	}
	return nil
}

func (c *Client) put(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("configcenter: write cancelled: %w", err)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if c.IsLocal() {
		c.cachePut(Item{Key: key, Value: value, UpdatedAt: time.Now().UTC()})
		return nil
	}
	path, err := c.environmentPath(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		return fmt.Errorf("configcenter: encode value: %w", err)
	}
	target := path + "/items/" + url.PathEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("configcenter: create write request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("configcenter: write request: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("configcenter: close write response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("configcenter: write status %d", resp.StatusCode)
	}
	item := Item{Key: key, Value: value, UpdatedAt: time.Now().UTC()}
	c.cachePut(item)
	return nil
}

func (c *Client) environmentPath(ctx context.Context) (string, error) {
	var scope struct {
		Project struct {
			Slug string `json:"slug"`
		} `json:"project"`
	}
	if err := c.get(ctx, "/project", nil, &scope); err != nil {
		return "", err
	}
	if scope.Project.Slug == "" {
		return "", errors.New("configcenter: missing project scope")
	}
	return strings.TrimSuffix(c.endpoint, "/agent") + "/projects/" + url.PathEscape(scope.Project.Slug) + "/environments/" + url.PathEscape(c.env), nil
}

func (c *Client) cachePut(item Item) {
	now := time.Now()
	c.mu.Lock()
	c.cache.prune(now)
	c.cache.put(item, now)
	c.cache.dropRevisions()
	snapshot := c.fileSnapshotLocked(c.prefix, now)
	c.mu.Unlock()
	c.persistSnapshot(snapshot)
}
