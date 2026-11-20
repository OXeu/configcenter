package configcenter

import (
	"context"
	"fmt"
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
	snapshot, err := LoadSnapshot(ctx, prefix, s...)
	return snapshot.All(), err
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
