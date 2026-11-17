package configcenter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPutUsesBoundProjectAndExplicitWritePermission(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cc_test" {
			t.Error("missing SDK authentication")
		}
		switch r.URL.Path {
		case "/api/v1/agent/project":
			json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"slug": "certcenter"}})
		case "/api/v1/projects/certcenter/environments/production/items/certcenter.domain.example.com":
			writes++
			if r.Method != http.MethodPut {
				t.Error("wrong method")
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["value"] != "{\"name\":\"example.com\"}" {
				t.Error("value changed")
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := New(Config{DotenvFiles: []string{}, BaseURL: server.URL, APIKey: "cc_test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Put(t.Context(), "certcenter.domain.example.com", "{\"name\":\"example.com\"}"); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatal("missing write")
	}
}
