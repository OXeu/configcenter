package configcenter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Snapshot is an immutable configuration view shared by local and remote readers.
// Its zero value is an empty snapshot.
type Snapshot struct {
	items map[string]Item
}

// LoadSnapshot reads sources in priority order and preserves explicit empty values.
// Source errors accompany any usable values; cancellation returns an empty view.
func LoadSnapshot(ctx context.Context, prefix string, sources ...DataSource) (Snapshot, error) {
	snapshot := Snapshot{items: make(map[string]Item)}
	var failures []error
	for i, source := range sources {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, fmt.Errorf("configcenter: read sources: %w", err)
		}
		items, err := source.Read(ctx, prefix)
		if err != nil {
			failures = append(failures, fmt.Errorf("configcenter: source %d: %w", i, err))
		}
		for _, item := range items {
			if _, exists := snapshot.items[item.Key]; !exists && strings.HasPrefix(item.Key, prefix) {
				snapshot.items[item.Key] = item
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("configcenter: read sources: %w", err)
	}
	return snapshot, errors.Join(failures...)
}

// Get returns a value and whether its key is present.
func (s Snapshot) Get(key string) (string, bool) {
	item, ok := s.items[key]
	return item.Value, ok
}

// GetDefault returns the value of key, including an empty string, or fallback.
func (s Snapshot) GetDefault(key, fallback string) string {
	if value, ok := s.Get(key); ok {
		return value
	}
	return fallback
}

// All returns a copy of the snapshot's items sorted by key.
func (s Snapshot) All() []Item {
	items := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}
