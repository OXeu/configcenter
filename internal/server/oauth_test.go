package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/config"
	"github.com/OXeu/configcenter/internal/store"
)

type fakeIdP struct {
	key               *rsa.PrivateKey
	server            *httptest.Server
	discoveryStatus   atomic.Int32
	discoveryRequests atomic.Int32
	// lastNonce is captured from the authorize request and echoed in the id token.
	lastNonce       string
	bindNonce       bool
	picture         string
	userinfoPicture string
	userinfoSubject string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &fakeIdP{key: key, bindNonce: true, picture: "https://images.example.com/dana.png", userinfoSubject: "u-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		idp.discoveryRequests.Add(1)
		if status := idp.discoveryStatus.Load(); status != 0 {
			http.Error(w, "private-provider-response", int(status))
			return
		}
		base := idp.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 base,
			"authorization_endpoint": base + "/authorize",
			"token_endpoint":         base + "/token",
			"jwks_uri":               base + "/jwks",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": idp.userinfoSubject, "picture": idp.userinfoPicture})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kid": "k1", "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != "good" {
			http.Error(w, "bad code", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": idp.sign(t, map[string]any{
			"iss":            idp.server.URL,
			"aud":            "client",
			"sub":            "u-1",
			"email":          "dana@example.com",
			"email_verified": true,
			"name":           "Dana",
			"picture":        idp.picture,
			"nonce":          idp.lastNonce,
			"iat":            time.Now().Unix(),
			"exp":            time.Now().Add(5 * time.Minute).Unix(),
			"groups":         []string{"configcenter-admin"},
		})})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (i *fakeIdP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func testServer(t *testing.T, withOAuth bool) (*Server, *gin.Engine, *fakeIdP) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Addr:        "127.0.0.1:0",
		DatabaseDSN: fmt.Sprintf("file:oauth-test-%d?mode=memory&cache=shared", time.Now().UnixNano()),
		PublicURL:   "http://127.0.0.1",
		SessionTTL:  time.Hour,
		Languages:   []string{"en", "zh-CN"},
		Bootstrap:   config.Bootstrap{AdminEmail: "admin@localhost", AdminName: "admin"},
		Groups:      auth.GroupPolicy{AdminGroups: []string{"configcenter-admin"}},
	}
	var idp *fakeIdP
	if withOAuth {
		idp = newFakeIdP(t)
		cfg.OAuth = auth.Config{
			IssuerURL: idp.server.URL, ClientID: "client", ClientSecret: "secret",
			RedirectURL: "http://127.0.0.1/oauth/callback", GroupsClaim: "groups",
		}
	}
	srv := newTestServer(t, cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	router := srv.router()
	return srv, router, idp
}

type jar struct{ cookies map[string]*http.Cookie }

func (j *jar) add(resp *http.Response) {
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 {
			delete(j.cookies, c.Name)
			continue
		}
		j.cookies[c.Name] = c
	}
}

func (j *jar) header() string {
	parts := make([]string, 0, len(j.cookies))
	for name, c := range j.cookies {
		parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(c.Value))
	}
	return strings.Join(parts, "; ")
}

func TestOAuthLoginCreatesAdminSession(t *testing.T) {
	srv, router, idp := testServer(t, true)
	cookies := &jar{cookies: map[string]*http.Cookie{}}

	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/login", http.NoBody))
	if start.Code != http.StatusFound {
		t.Fatalf("start status = %d", start.Code)
	}
	cookies.add(start.Result())
	authURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if authURL.Query().Get("code_challenge") == "" || authURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("missing pkce parameters: %s", authURL)
	}
	state := authURL.Query().Get("state")
	idp.lastNonce = authURL.Query().Get("nonce")

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=good&state="+url.QueryEscape(state), http.NoBody)
	req.Header.Set("Cookie", cookies.header())
	cb := httptest.NewRecorder()
	router.ServeHTTP(cb, req)
	if cb.Code != http.StatusFound || cb.Header().Get("Location") != "/projects" {
		t.Fatalf("callback = %d %q", cb.Code, cb.Header().Get("Location"))
	}
	cookies.add(cb.Result())

	me := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody)
	meReq.Header.Set("Cookie", cookies.header())
	router.ServeHTTP(me, meReq)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", me.Code, me.Body.String())
	}
	var body struct {
		Email     string `json:"email"`
		Role      string `json:"role"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if body.Email != "dana@example.com" || body.Role != "admin" || body.AvatarURL != idp.picture {
		t.Fatalf("me = %+v", body)
	}
	for _, avatar := range []string{"https://images.example.com/updated.png", ""} {
		updated, updateErr := srv.store.LinkOAuthUser(context.Background(), store.Identity{Issuer: idp.server.URL, Sub: "u-1", Email: body.Email, Name: "Dana", AvatarURL: avatar, Role: store.RoleUser, Now: time.Now().UTC()})
		if updateErr != nil {
			t.Fatal(updateErr)
		}
		if updated.AvatarURL != avatar || updated.Role != store.RoleAdmin {
			t.Fatalf("updated profile = %+v", updated)
		}
	}
}

func TestOAuthRejectsStateMismatch(t *testing.T) {
	_, router, _ := testServer(t, true)
	cookies := &jar{cookies: map[string]*http.Cookie{}}
	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/login", http.NoBody))
	cookies.add(start.Result())

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=good&state=forged", http.NoBody)
	req.Header.Set("Cookie", cookies.header())
	cb := httptest.NewRecorder()
	router.ServeHTTP(cb, req)
	if got := cb.Header().Get("Location"); got != "/login?error=oauth_failed" {
		t.Fatalf("redirect = %q", got)
	}
}

func TestSessionCookieRequiredForAPI(t *testing.T) {
	_, router, _ := testServer(t, false)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.Code)
	}
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/projects", http.NoBody))
	if page.Code != http.StatusFound || page.Header().Get("Location") != "/login" {
		t.Fatalf("page = %d %q", page.Code, page.Header().Get("Location"))
	}
}

func TestOAuthUserinfoPictureMatchesSubject(t *testing.T) {
	for _, subject := range []string{"u-1", "someone-else"} {
		t.Run(subject, func(t *testing.T) {
			srv, router, idp := testServer(t, true)
			idp.picture = ""
			idp.userinfoPicture = "https://images.example.com/userinfo.png"
			idp.userinfoSubject = subject
			start := httptest.NewRecorder()
			router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/login", http.NoBody))
			authURL, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			idp.lastNonce = authURL.Query().Get("nonce")
			cookies := &jar{cookies: map[string]*http.Cookie{}}
			cookies.add(start.Result())
			req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=good&state="+url.QueryEscape(authURL.Query().Get("state")), http.NoBody)
			req.Header.Set("Cookie", cookies.header())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Header().Get("Location") != "/projects" {
				t.Fatalf("login = %d %s", response.Code, response.Header().Get("Location"))
			}
			user, err := srv.store.UserBySubject(context.Background(), idp.server.URL, "u-1")
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if subject == "u-1" {
				want = idp.userinfoPicture
			}
			if user.AvatarURL != want {
				t.Fatalf("picture = %q, want %q", user.AvatarURL, want)
			}
		})
	}
}
