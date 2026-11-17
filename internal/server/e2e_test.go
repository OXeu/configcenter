package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/center-common/oauth"
	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/config"
	"github.com/OXeu/configcenter/internal/store"
)

func TestMain(m *testing.M) {
	os.Exit(runWithScratch(m))
}

type flows struct {
	t       *testing.T
	router  http.Handler
	cookies map[string]*http.Cookie
	csrf    string
}

func (f *flows) do(method, path string, body any) (status int, decoded map[string]any) {
	f.t.Helper()
	var payload strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			f.t.Fatalf("encode body: %v", err)
		}
		payload = *strings.NewReader(string(encoded))
	}
	req := httptest.NewRequest(method, path, &payload)
	if len(f.cookies) > 0 {
		parts := make([]string, 0, len(f.cookies))
		for name, c := range f.cookies {
			parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(c.Value))
		}
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	if f.csrf != "" {
		req.Header.Set("X-CSRF-Token", f.csrf)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(f.cookies, c.Name)
			continue
		}
		f.cookies[c.Name] = c
	}
	_ = json.Unmarshal(w.Body.Bytes(), &decoded)
	return w.Code, decoded
}

func newFlows(t *testing.T) *flows {
	t.Helper()
	gin.SetMode(gin.TestMode)
	idp := newFakeIdP(t)
	cfg := config.Config{
		Addr:        "127.0.0.1:0",
		DatabaseDSN: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		PublicURL:   "http://cc.test",
		SessionTTL:  time.Hour,
		Languages:   []string{"en", "zh-CN"},
		Bootstrap:   config.Bootstrap{AdminEmail: "admin@localhost", AdminName: "admin"},
		Groups:      auth.GroupPolicy{AdminGroups: []string{"configcenter-admin"}},
		OAuth: auth.Config{
			IssuerURL: idp.server.URL, ClientID: "client", ClientSecret: "secret",
			RedirectURL: "http://cc.test/oauth/callback", GroupsClaim: "groups",
		},
	}
	srv, err := newServer(context.Background(), cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	f := &flows{t: t, router: srv.router(), cookies: map[string]*http.Cookie{}}

	code, _ := f.do(http.MethodGet, "/auth/login", nil)
	if code != http.StatusFound {
		t.Fatalf("login status = %d", code)
	}
	state, nonce := startState(t, f)
	idp.lastNonce = nonce
	code, _ = f.do(http.MethodGet, "/oauth/callback?code=good&state="+url.QueryEscape(state), nil)
	if code != http.StatusFound {
		t.Fatalf("callback status = %d", code)
	}
	_, me := f.do(http.MethodGet, "/api/v1/me", nil)
	token, _ := me["csrf"].(string)
	f.csrf = token
	return f
}

func startState(t *testing.T, f *flows) (state, nonce string) {
	t.Helper()
	raw := f.cookies[oauthStateCookie]
	if raw == nil {
		t.Fatal("missing oauth state cookie")
	}
	sc, err := oauth.DecodeState(raw.Value)
	if err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return sc.State, sc.Nonce
}

func TestWebAndAgentFlowsAreProjectScoped(t *testing.T) {
	f := newFlows(t)

	code, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]any{"slug": "billing", "name": "Billing"})
	if code != http.StatusCreated {
		t.Fatalf("create project = %d", code)
	}
	code, created := f.do(http.MethodPost, "/api/v1/keys", scopeBody("agent", keyScopeJSON{Project: "billing", Permissions: []store.Permission{store.ConfigRead, store.ConfigWrite, store.ProposalsWrite}}))
	if code != http.StatusCreated {
		t.Fatalf("create key = %d", code)
	}
	token, _ := created["token"].(string)
	if !strings.HasPrefix(token, "cc_") {
		t.Fatalf("token = %q", token)
	}

	agent := func(project, tool string, args map[string]any) map[string]any {
		t.Helper()
		args["project"] = project
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		var reply map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return reply
	}

	set := agent("billing", "set_config", map[string]any{"environment": "production", "key": "db.pool", "value": "20"})
	if result, _ := set["result"].(map[string]any); result == nil || result["isError"] == true {
		t.Fatalf("set_config failed: %v", set)
	}
	elsewhere := agent("other", "list_config", map[string]any{"environment": "production"})
	if result, _ := elsewhere["result"].(map[string]any); result == nil || result["isError"] != true {
		t.Fatalf("a key of billing reached another project: %v", elsewhere)
	}

	code, items := f.do(http.MethodGet, "/api/v1/projects/billing/environments/production/items", nil)
	if code != http.StatusOK || len(items["items"].([]any)) != 1 {
		t.Fatalf("items = %d %v", code, items)
	}

	proposed := agent("billing", "propose_config", map[string]any{
		"environment": "production", "key": "db.pool", "action": "set", "value": "5", "reason": "tune",
	})
	if proposed["result"] == nil {
		t.Fatalf("propose failed: %v", proposed)
	}
	code, list := f.do(http.MethodGet, "/api/v1/projects/billing/proposals?status=pending", nil)
	pending, _ := list["proposals"].([]any)
	if code != http.StatusOK || len(pending) != 1 {
		t.Fatalf("proposals = %d %v", code, list)
	}
	id, _ := pending[0].(map[string]any)
	code, reviewed := f.do(http.MethodPost, fmt.Sprintf("/api/v1/projects/billing/proposals/%v/review", id["id"]), map[string]any{"approve": true})
	proposal, _ := reviewed["proposal"].(map[string]any)
	if code != http.StatusOK || proposal["status"] != "applied" {
		t.Fatalf("review = %d %v", code, reviewed)
	}
	if got := agent("billing", "get_config", map[string]any{"environment": "production", "key": "db.pool"}); got["result"] == nil {
		t.Fatalf("get_config failed: %v", got)
	}

	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"slug":"no-csrf","name":"x"}`))
	withoutCSRF.Header.Set("Cookie", "cc_session="+f.cookies["cc_session"].Value)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, withoutCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf = %d", rec.Code)
	}
}

func TestPagesRenderForSignedInUser(t *testing.T) {
	f := newFlows(t)
	if code, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]any{"slug": "billing", "name": "Billing"}); code != http.StatusCreated {
		t.Fatalf("create project = %d", code)
	}
	for _, path := range []string{"/projects", "/projects/billing", "/users"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		req.Header.Set("Cookie", "cc_session="+f.cookies["cc_session"].Value)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, w.Code)
		}
		page := w.Body.String()
		if !strings.Contains(page, `id="page-data"`) || !strings.Contains(page, `id="app"`) {
			t.Fatalf("%s did not render the Vue shell", path)
		}

	}
}

func TestLanguageSwitcherAndNegotiation(t *testing.T) {
	f := newFlows(t)
	session := "cc_session=" + f.cookies["cc_session"].Value

	page := func(header http.Header) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
		req.Header.Set("Cookie", session)
		for name, values := range header {
			req.Header[name] = values
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("projects page = %d", w.Code)
		}
		return w.Body.String()
	}

	english := page(nil)
	if !strings.Contains(english, "New project") || !strings.Contains(english, `lang="en"`) {
		t.Fatal("default page is not English")
	}

	chinese := page(http.Header{"Cookie": {"cc_session=" + f.cookies["cc_session"].Value + "; cc_lang=zh-CN"}})
	if !strings.Contains(chinese, "新建项目") || !strings.Contains(chinese, `lang="zh-CN"`) {
		t.Fatal("cookie did not switch the page to Chinese")
	}
	if !strings.Contains(chinese, "退出登录") || !strings.Contains(chinese, `"tag":"en"`) || !strings.Contains(chinese, `"tag":"zh-CN"`) {
		t.Fatal("language menu does not list every enabled language")
	}

	negotiated := page(http.Header{"Cookie": {"cc_session=" + f.cookies["cc_session"].Value}, "Accept-Language": {"zh-CN,zh;q=0.9,en;q=0.5"}})
	if !strings.Contains(negotiated, "新建项目") {
		t.Fatal("Accept-Language was not negotiated")
	}

	req := httptest.NewRequest(http.MethodGet, "/language?lang=zh-CN", http.NoBody)
	req.Header.Set("Referer", "http://cc.test/projects/billing")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects/billing" {
		t.Fatalf("switch = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if cookie := rec.Result().Cookies(); len(cookie) == 0 || cookie[0].Name != "cc_lang" || cookie[0].Value != "zh-CN" {
		t.Fatalf("switch did not store the language: %v", cookie)
	}

	for _, bad := range []string{"?lang=fr", "?lang="} {
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/language"+bad, http.NoBody))
		if got := rec.Result().Cookies(); len(got) == 0 || got[0].Value != "en" {
			t.Fatalf("%s selected %v", bad, got)
		}
		if got := rec.Header().Get("Location"); got != "/projects" {
			t.Fatalf("%s redirected to %q", bad, got)
		}
	}
}

func TestVuePageContractAndEscaping(t *testing.T) {
	f := newFlows(t)
	name := `</script><script>alert("x")</script>`
	if code, _ := f.do(http.MethodPost, "/api/v1/projects", map[string]any{"slug": "escaped", "name": name}); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "/projects/escaped", http.NoBody)
	req.Header.Set("Cookie", "cc_session="+f.cookies["cc_session"].Value)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	_, tail, found := strings.Cut(w.Body.String(), `<script id="page-data" type="application/json">`)
	if !found {
		t.Fatal("missing JSON bootstrap")
	}
	raw, _, found := strings.Cut(tail, "</script>")
	if !found || strings.Contains(raw, "<script>") {
		t.Fatal("unsafe script boundary")
	}
	var data struct {
		Page     string `json:"page"`
		CanWrite bool   `json:"canWrite"`
		CSRF     string `json:"csrf"`
		Nonce    string `json:"nonce"`
		Me       struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"me"`
		Project struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	if data.Page != "project" || data.Project.Slug != "escaped" || data.Project.Name != name || data.Me.Role != "admin" || !data.CanWrite || data.CSRF == "" {
		t.Fatalf("invalid page contract: %+v", data)
	}
	if data.Nonce == "" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+data.Nonce+"'") {
		t.Fatal("missing style nonce")
	}
	if strings.Contains(raw, "OAuthSub") || strings.Contains(raw, "OAuthIssuer") {
		t.Fatal("private model fields exposed")
	}
	matches := regexp.MustCompile(`/static/app/[^"]+\.(?:js|css)`).FindAllString(w.Body.String(), -1)
	if len(matches) < 2 {
		t.Fatal("missing hashed frontend assets")
	}
	sprites := make(map[string]struct{})
	for _, path := range matches {
		if !strings.HasSuffix(path, ".css") {
			continue
		}
		css := httptest.NewRecorder()
		f.router.ServeHTTP(css, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		for _, sprite := range regexp.MustCompile(`/static/app/remixicon-[a-f0-9]{16}\.svg`).FindAllString(css.Body.String(), -1) {
			sprites[sprite] = struct{}{}
		}
	}
	if len(sprites) != 1 {
		t.Fatalf("expected one hashed sprite URL in CSS, got %v", sprites)
	}
	for sprite := range sprites {
		matches = append(matches, sprite)
	}
	for _, path := range append(matches, "/static/brand/icon-32.webp", "/static/brand/icon-512.webp") {
		asset := httptest.NewRecorder()
		f.router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
			t.Fatalf("asset %s = %d", path, asset.Code)
		}
		expected := "public, max-age=3600"
		if strings.HasPrefix(path, "/static/app/") {
			expected = "public, max-age=31536000, immutable"
		}
		if asset.Header().Get("Cache-Control") != expected {
			t.Fatalf("asset %s cache policy = %s", path, asset.Header().Get("Cache-Control"))
		}
	}
}
