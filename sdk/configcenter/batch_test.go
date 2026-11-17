package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestBatchRemoteReadsAndWrites(t *testing.T) {
	var mu sync.Mutex
	reads, writes := 0, 0
	values := map[string]string{"keep": "before", "untouched": "retained"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer cc_batch" {
			t.Error("missing API key")
		}
		switch r.URL.Path {
		case "/api/v1/agent/project":
			json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "app"}})
		case "/api/v1/agent/environments/production/values":
			reads++
			keys := r.URL.Query()["keys"]
			if !reflect.DeepEqual(keys, []string{"keep", "new/key", "missing"}) {
				t.Error("selection missing", keys)
			}
			items := []Item{}
			for _, key := range keys {
				if value, ok := values[key]; ok {
					items = append(items, Item{Key: key, Value: value})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items})
		case "/api/v1/projects/app/environments/production/items":
			if r.Method != http.MethodPut {
				t.Error("wrong batch method", r.Method)
			}
			var payload struct {
				Items []Item `json:"items"`
			}
			json.NewDecoder(r.Body).Decode(&payload)
			if len(payload.Items) != 2 {
				t.Error("not one batch", len(payload.Items))
			}
			for _, item := range payload.Items {
				values[item.Key] = item.Value
			}
			writes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "cc_batch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PutMany(t.Context(), map[string]string{"keep": "after", "new/key": ""}); err != nil {
		t.Fatal(err)
	}
	got, err := client.ReadMany(t.Context(), []string{"keep", "new/key", "missing"})
	if err != nil || !reflect.DeepEqual(got, map[string]string{"keep": "after", "new/key": ""}) {
		t.Fatal(got, err)
	}
	if client.GetDefault("keep", "") != "after" {
		t.Fatal("batch cache not updated")
	}
	if err := client.PutMany(t.Context(), map[string]string{"bad key": "secret-value"}); err == nil || !strings.Contains(err.Error(), "bad key") || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("invalid error", err)
	}
	if _, err := client.ReadMany(t.Context(), []string{"keep", "keep"}); err == nil {
		t.Fatal("duplicate keys accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	if reads != 1 || writes != 1 || values["untouched"] != "retained" {
		t.Fatal("per-key requests or unrelated update", reads, writes)
	}
}

func TestBatchLocalValidationAndCancellation(t *testing.T) {
	client, err := New(Config{DotenvFiles: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PutMany(t.Context(), map[string]string{"keep": "original", "new/key": ""}); err != nil {
		t.Fatal(err)
	}
	if err := client.PutMany(t.Context(), map[string]string{"keep": "changed", "bad key": "invalid"}); err == nil {
		t.Fatal("bad batch accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.PutMany(ctx, map[string]string{"keep": "cancelled"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := client.ReadMany(ctx, []string{"keep"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	values, err := client.ReadMany(t.Context(), []string{"keep", "new/key", "missing"})
	if err != nil || !reflect.DeepEqual(values, map[string]string{"keep": "original", "new/key": ""}) {
		t.Fatal("partial write", values, err)
	}
	if err := client.PutMany(t.Context(), nil); err == nil {
		t.Fatal("empty batch accepted")
	}
	if _, err := client.ReadMany(t.Context(), nil); err == nil {
		t.Fatal("empty selection accepted")
	}
}
