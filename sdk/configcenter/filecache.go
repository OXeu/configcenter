package configcenter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// fileCacheVersion identifies the persisted snapshot layout.
const fileCacheVersion = 1

type fileSnapshot struct {
	FetchedAt   time.Time `json:"fetched_at"`
	Environment string    `json:"environment"`
	Prefix      string    `json:"prefix"`
	Revision    string    `json:"revision"`
	Version     int       `json:"version"`
	Items       []Item    `json:"items"`
}

func readFileSnapshot(path string) (fileSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileSnapshot{}, nil
		}
		return fileSnapshot{}, fmt.Errorf("configcenter: read local cache %q: %w", path, err)
	}
	var persisted fileSnapshot
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fileSnapshot{}, fmt.Errorf("configcenter: decode local cache %q: %w", path, err)
	}
	if persisted.Version != fileCacheVersion {
		return fileSnapshot{}, nil
	}
	return persisted, nil
}

func encodeFileSnapshot(snapshot fileSnapshot) ([]byte, error) {
	snapshot.Version = fileCacheVersion
	snapshot.Items = sortedItems(snapshot.Items)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("configcenter: encode local cache: %w", err)
	}
	return encoded, nil
}

// writeFileSnapshot replaces path atomically so a crash never leaves a partial
// cache behind.
func writeFileSnapshot(path string, encoded []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("configcenter: create local cache directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("configcenter: create local cache: %w", err)
	}
	name := temp.Name()
	if _, err := temp.Write(encoded); err != nil {
		return errors.Join(fmt.Errorf("configcenter: write local cache: %w", err), temp.Close(), os.Remove(name))
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("configcenter: sync local cache: %w", err), temp.Close(), os.Remove(name))
	}
	if err := temp.Close(); err != nil {
		return errors.Join(fmt.Errorf("configcenter: close local cache: %w", err), os.Remove(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(fmt.Errorf("configcenter: replace local cache: %w", err), os.Remove(name))
	}
	return nil
}

func sortedItems(items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// digestOf hashes encoded bytes for change detection that never reaches the wire.
func digestOf(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// revisionOf derives a strong validator from configuration content alone. Map
// ordering and commit timestamps never leak into the result, so an unchanged
// environment yields the same value and a rewritten item with the same content
// is not reported as a change.
func revisionOf(items []Item, extra ...string) string {
	hash := sha256.New()
	for _, item := range sortedItems(items) {
		if _, err := fmt.Fprintf(hash, "%q\x00%q\x00%t\x00%s\x00%q\n", item.Key, item.Value, item.Secret, item.Description, extra); err != nil {
			continue
		}
	}
	return "\"" + hex.EncodeToString(hash.Sum(nil)) + "\""
}
