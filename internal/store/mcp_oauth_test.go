package store

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
)

func mcpStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), sqlite.Open(":memory:"), "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func issueMCPStoreToken(t *testing.T, s *Store, now time.Time) (MCPAuthorization, MCPToken) {
	t.Helper()
	digest := "code-digest"
	request := MCPAuthorization{ID: "grant", ClientID: "client", UserID: 1, Resource: "https://cc.example/mcp", Approved: true, CodeDigest: &digest, ExpiresAt: now.Add(5 * time.Minute)}
	if err := s.CreateMCPAuthorization(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	token := MCPToken{GrantID: request.ID, AccessDigest: "access", RefreshDigest: "refresh", ExpiresAt: now.Add(time.Hour)}
	if err := s.ExchangeMCPCode(t.Context(), request, token, now); err != nil {
		t.Fatal(err)
	}
	return request, token
}

func TestMCPTokenExpiryAudienceAndPersistence(t *testing.T) {
	s := mcpStore(t)
	now := time.Now().UTC()
	request, token := issueMCPStoreToken(t, s, now)
	for _, tc := range []struct {
		resource string
		at       time.Time
	}{
		{"https://other.example/mcp", now},
		{request.Resource, now.Add(time.Hour)},
		{request.Resource, now.Add(31 * 24 * time.Hour)},
	} {
		if _, err := s.MCPTokenUser(t.Context(), token.AccessDigest, tc.resource, tc.at); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid audience or expiration accepted: %v", err)
		}
	}
	reopened := &Store{db: s.db}
	user, err := reopened.MCPTokenUser(t.Context(), token.AccessDigest, request.Resource, now)
	if err != nil || user.ID != 1 {
		t.Fatal("token depended on process-local state")
	}
	if _, err := s.MCPAuthorizationByCode(t.Context(), *request.CodeDigest, now.Add(5*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired code accepted")
	}
	if _, err := s.MCPGrantByID(t.Context(), request.ID, now.Add(30*24*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired refresh grant accepted")
	}
}

func TestMCPLoginBindingAndConsentExpiry(t *testing.T) {
	s := mcpStore(t)
	now := time.Now().UTC()
	request := MCPAuthorization{ID: "request", ExpiresAt: now.Add(time.Minute)}
	if err := s.CreateMCPAuthorization(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := s.BindMCPLogin(t.Context(), request.ID, "login-state"); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteMCPLogin(t.Context(), request.ID, "wrong-state", 1, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("login state substitution succeeded")
	}
	if err := s.CompleteMCPLogin(t.Context(), request.ID, "login-state", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteMCPLogin(t.Context(), request.ID, "login-state", 2, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("login binding replay succeeded")
	}
	code := "digest"
	if err := s.ApproveMCPAuthorization(t.Context(), request.ID, 2, &code, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("another user approved consent")
	}
	if err := s.ApproveMCPAuthorization(t.Context(), request.ID, 1, &code, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired consent was accepted")
	}
}

func TestMCPConcurrentRefreshRevokesFamily(t *testing.T) {
	s := mcpStore(t)
	now := time.Now().UTC()
	request, previous := issueMCPStoreToken(t, s, now)
	var successes atomic.Int32
	var workers sync.WaitGroup
	for _, suffix := range []string{"a", "b"} {
		workers.Go(func() {
			next := MCPToken{GrantID: request.ID, AccessDigest: "access-" + suffix, RefreshDigest: "refresh-" + suffix, ExpiresAt: now.Add(time.Hour)}
			err := s.RotateMCPToken(t.Context(), previous, next, now)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrNotFound) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if successes.Load() != 1 {
		t.Fatalf("refresh issued %d token pairs", successes.Load())
	}
	for _, access := range []string{"access", "access-a", "access-b"} {
		if _, err := s.MCPTokenUser(t.Context(), access, request.Resource, now); !errors.Is(err, ErrNotFound) {
			t.Fatal("refresh replay left a valid family token")
		}
	}
}
