package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteStatusFailuresLoadFallbacksAndReportAfterUnlock(t *testing.T) {
	for _, status := range []int{443, http.StatusServiceUnavailable, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private-response-secret", status)
			}))
			defer server.Close()
			file := dotenvTestFile(t, "db.pool=12\ndb.empty=\nother=hidden\n")
			var reported error
			var client *Client
			client = testClient(t, Config{
				BaseURL: server.URL, APIKey: "cc_private-token", DotenvFiles: []string{file},
				Defaults: map[string]string{"db.pool": "2", "db.timeout": "30", "db.empty": "default"},
				OnError: func(err error) {
					reported = err
					if client.Int("db.pool", 0) != 12 {
						t.Error("error callback ran before publishing fallbacks")
					}
					ctx, cancel := context.WithCancel(t.Context())
					cancel()
					if err := client.Load(ctx); !errors.Is(err, context.Canceled) {
						t.Error("reentrant cancelled load lost its error", err)
					}
				},
			})
			if err := client.LoadPrefix(t.Context(), "db."); err != nil {
				t.Fatal("remote outage blocked startup", err)
			}
			if reported == nil || strings.Contains(reported.Error(), "private") {
				t.Fatal("remote diagnostic was missing or disclosed credentials or response content")
			}
			if status == http.StatusNotFound && !errors.Is(reported, ErrNotFound) {
				t.Fatal("reported 404 lost its error identity")
			}
			if client.Int("db.timeout", 0) != 30 || client.GetDefault("db.empty", "missing") != "" || len(client.All()) != 3 {
				t.Fatal("fallback precedence, explicit empty values or prefix filtering changed")
			}
		})
	}
}

func TestRemoteFailureAllowsCallerDefaultsAndRetry(t *testing.T) {
	var available atomic.Bool
	var reports atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !available.Load() {
			w.WriteHeader(443)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "key", Value: "recovered"}}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}, OnError: func(error) { reports.Add(1) }})
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "caller-default") != "caller-default" || len(client.All()) != 0 {
		t.Fatal("remote failure blocked caller defaults", err)
	}
	available.Store(true)
	if err := client.Load(t.Context()); err != nil || client.GetDefault("key", "") != "recovered" || reports.Load() != 1 {
		t.Fatal("failed remote read was cached or did not recover", err)
	}
}

func TestRemoteTransportFailureDiffersFromCallerCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("connection refused"), context.DeadlineExceeded} {
		var reported error
		client := testClient(t, Config{
			BaseURL: "https://config.example", APIKey: "cc_test", DotenvFiles: []string{},
			Defaults: map[string]string{"key": "default"}, OnError: func(err error) { reported = err },
			HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, failure })},
		})
		if err := client.Load(t.Context()); err != nil || !errors.Is(reported, failure) || client.GetDefault("key", "") != "default" {
			t.Fatal("transport failure blocked fallbacks or lost its diagnostic", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := testClient(t, Config{
		BaseURL: "https://config.example", APIKey: "cc_test", DotenvFiles: []string{},
		OnError: func(error) { t.Error("caller cancellation was treated as a nonfatal outage") },
		HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			cancel()
			return nil, context.Canceled
		})},
	})
	if err := client.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation was swallowed", err)
	}
}

func TestRemoteFailureDoesNotHideInvalidLocalConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(443) }))
	defer server.Close()
	file := dotenvTestFile(t, "!invalid=private-local-secret\n")
	client := testClient(t, Config{BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{file}, Defaults: map[string]string{"key": "default"}})
	if err := client.Load(t.Context()); !errors.Is(err, ErrInvalidDotenv) || !strings.Contains(err.Error(), "443") || strings.Contains(err.Error(), "private-local-secret") {
		t.Fatal("local failure was hidden or leaked its contents", err)
	}
	if client.GetDefault("key", "") != "default" {
		t.Fatal("local failure lost usable defaults")
	}
	if err := client.Watch(t.Context(), WatchOptions{}); !errors.Is(err, ErrInvalidDotenv) {
		t.Fatal("invalid local configuration started a watcher", err)
	}
}

func TestWatchStartsDuringRemoteOutageAndRecovers(t *testing.T) {
	var available atomic.Bool
	var clientReports, watchReports atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !available.Load() {
			w.WriteHeader(443)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"items": []Item{{Key: "key", Value: "recovered"}}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{
		BaseURL: server.URL, APIKey: "cc_test", DotenvFiles: []string{}, Defaults: map[string]string{"key": "default"},
		OnError: func(error) { clientReports.Add(1) },
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		client.opMu.Lock()
		defer client.opMu.Unlock()
	}()
	if err := client.Watch(ctx, WatchOptions{Interval: 5 * time.Millisecond, OnError: func(error) { watchReports.Add(1) }}); err != nil {
		t.Fatal("remote outage blocked watcher startup", err)
	}
	if client.GetDefault("key", "") != "default" || watchReports.Load() != 1 || clientReports.Load() != 0 {
		t.Fatal("watcher did not publish fallbacks and report its initial failure")
	}
	available.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.GetDefault("key", "") == "recovered" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("watcher stopped polling after its initial failure")
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
