package configcenter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExplicitSecretReads(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		t.Run(map[bool]string{false: "masked", true: "privileged"}[privileged], func(t *testing.T) {
			want := "sk-****x123"
			path := "/api/v1/agent/environments/production/values"
			if privileged {
				want = "sk-confidential-middle-x123"
				path += "/reveal"
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != path || r.Header.Get("Authorization") != "Bearer cc_test" {
					t.Error("SDK did not use the explicitly selected read path")
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "token", Value: want, Secret: true, Masked: !privileged}}}); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			client, err := New(Config{BaseURL: srv.URL, APIKey: "cc_test", RevealSecrets: privileged, DotenvFiles: []string{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			items := client.All()
			if len(items) != 1 || items[0].Value != want || items[0].Masked == privileged || !items[0].Secret {
				t.Fatal("SDK lost masking metadata")
			}
			values, err := client.ReadMany(t.Context(), []string{"token"})
			if err != nil || values["token"] != want || calls != 2 {
				t.Fatal("batch read did not use the selected privilege level", err)
			}
		})
	}
}

func TestSecretReadDenialDoesNotFallBackToOrdinaryRead(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, APIKey: "cc_test", RevealSecrets: true, DotenvFiles: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Load(t.Context()); err == nil || calls != 1 {
		t.Fatal("denied privileged read was silently retried")
	}
	if _, ok := client.Get("token"); ok {
		t.Fatal("denied read populated the cache")
	}
}
