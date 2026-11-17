package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
)

func TestPutItemsRollsBackEarlierChunks(t *testing.T) {
	s, err := Open(t.Context(), sqlite.Open(":memory:"), "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.PutItem(t.Context(), ItemInput{ProjectID: 1, EnvironmentID: 1, Key: "keep", Value: "original", UpdatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.WithContext(t.Context()).Exec("CREATE TRIGGER reject_batch BEFORE INSERT ON config_items WHEN NEW.key = 'reject' BEGIN SELECT RAISE(ABORT, 'rejected batch'); END").Error; err != nil {
		t.Fatal(err)
	}
	items := []ItemInput{{ProjectID: 1, EnvironmentID: 1, Key: "keep", Value: "changed", UpdatedBy: 1}}
	for i := 0; i < 100; i++ {
		items = append(items, ItemInput{ProjectID: 1, EnvironmentID: 1, Key: fmt.Sprintf("new.%d", i), Value: "value", UpdatedBy: 1})
	}
	items = append(items, ItemInput{ProjectID: 1, EnvironmentID: 1, Key: "reject", Value: "value", UpdatedBy: 1})
	if err := s.PutItems(t.Context(), items); err == nil {
		t.Fatal("failing batch committed")
	}
	kept, err := s.GetItem(t.Context(), 1, 1, "keep")
	if err != nil || kept.Value != "original" || !kept.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatal("earlier update not rolled back", err)
	}
	if _, err := s.GetItem(t.Context(), 1, 1, "new.0"); !errors.Is(err, ErrNotFound) {
		t.Fatal("earlier insert not rolled back", err)
	}
	items = items[:len(items)-1]
	if err := s.PutItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	selected, err := s.GetItems(t.Context(), 1, 1, []string{"keep", "new.99"})
	if err != nil || len(selected) != 2 || selected[0].Value != "changed" {
		t.Fatal("multi-chunk upsert failed", err)
	}
}
