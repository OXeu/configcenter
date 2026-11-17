package configcenter

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// DataSource reads a snapshot restricted to prefix. Empty values are present values.
// A source may return usable items alongside an error.
type DataSource interface {
	Read(ctx context.Context, prefix string) ([]Item, error)
}

// Sources composes data sources in descending priority, retaining the first value
// for each key. Read returns available values together with all source errors.
type Sources []DataSource

// Read merges matching values without treating empty strings as missing.
// Cancellation aborts the read without returning a partial snapshot.
func (s Sources) Read(ctx context.Context, prefix string) ([]Item, error) {
	values := make(map[string]Item)
	var failures []error
	for i, source := range s {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("configcenter: read sources: %w", err)
		}
		items, err := source.Read(ctx, prefix)
		if err != nil {
			failures = append(failures, fmt.Errorf("configcenter: source %d: %w", i, err))
		}
		for _, item := range items {
			if _, exists := values[item.Key]; !exists && strings.HasPrefix(item.Key, prefix) {
				values[item.Key] = item
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("configcenter: read sources: %w", err)
	}
	items := make([]Item, 0, len(values))
	for _, item := range values {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, errors.Join(failures...)
}

// MapSource supplies in-memory configuration such as application defaults.
// Callers must not mutate the map during Read.
type MapSource map[string]string

// Read returns map entries matching prefix.
func (s MapSource) Read(ctx context.Context, prefix string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("configcenter: read map: %w", err)
	}
	items := make([]Item, 0, len(s))
	for key, value := range s {
		if strings.HasPrefix(key, prefix) {
			items = append(items, Item{Key: key, Value: value})
		}
	}
	return items, nil
}

type remoteSource struct {
	client *Client
}

func (s remoteSource) Read(ctx context.Context, prefix string) ([]Item, error) {
	query := url.Values{}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	var body struct {
		Items []Item `json:"items"`
	}
	path := s.client.valuesPath()
	if err := s.client.get(ctx, path, query, &body); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(body.Items))
	for _, item := range body.Items {
		if err := validateKey(item.Key); err != nil {
			return nil, fmt.Errorf("configcenter: invalid remote key: %w", err)
		}
		if strings.HasPrefix(item.Key, prefix) {
			items = append(items, item)
		}
	}
	return items, nil
}
