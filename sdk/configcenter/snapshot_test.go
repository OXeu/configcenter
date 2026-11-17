package configcenter

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSnapshotUsesSharedSourcePrecedence(t *testing.T) {
	first := MapSource{"CC_EMPTY": "", "CC_ISSUER": "stored", "other": "excluded"}
	second := MapSource{"CC_EMPTY": "local", "CC_ISSUER": "local", "CC_FALLBACK": "fallback"}
	snapshot, err := LoadSnapshot(t.Context(), "CC_", first, second)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GetDefault("CC_EMPTY", "default") != "" || snapshot.GetDefault("CC_ISSUER", "default") != "stored" {
		t.Fatal("snapshot lost priority or an explicit empty value")
	}
	if snapshot.GetDefault("CC_FALLBACK", "default") != "fallback" || snapshot.GetDefault("missing", "default") != "default" {
		t.Fatal("snapshot did not preserve fallbacks")
	}
	if _, ok := snapshot.Get("other"); ok {
		t.Fatal("snapshot escaped its prefix")
	}
	items, err := (Sources{first, second}).Read(t.Context(), "CC_")
	if err != nil || !reflect.DeepEqual(items, snapshot.All()) {
		t.Fatal("source composition and snapshots disagree", err)
	}
	first["CC_ISSUER"] = "changed"
	returned := snapshot.All()
	returned[0].Value = "changed"
	if snapshot.GetDefault("CC_ISSUER", "") != "stored" || snapshot.GetDefault("CC_EMPTY", "default") != "" {
		t.Fatal("snapshot shares mutable input or returned items")
	}
}

func TestSnapshotErrorsAndCancellation(t *testing.T) {
	failure := errors.New("source unavailable")
	snapshot, err := LoadSnapshot(t.Context(), "", failingSource{err: failure}, MapSource{"fallback": "value"})
	if !errors.Is(err, failure) || snapshot.GetDefault("fallback", "") != "value" {
		t.Fatal("snapshot lost usable values or source errors", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	snapshot, err = LoadSnapshot(ctx, "", MapSource{"fallback": "value"})
	if !errors.Is(err, context.Canceled) || len(snapshot.All()) != 0 {
		t.Fatal("cancelled snapshot exposed partial values", err)
	}
	var empty Snapshot
	if value, ok := empty.Get("key"); ok || value != "" || len(empty.All()) != 0 {
		t.Fatal("zero snapshot was not empty")
	}
}
