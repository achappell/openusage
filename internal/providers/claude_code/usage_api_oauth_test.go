package claude_code

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// setTempHome points os.UserHomeDir at a fresh temp dir. HOME covers
// Unix-likes; USERPROFILE is what os.UserHomeDir reads on Windows.
func setTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeCredentials drops a fake ~/.claude/.credentials.json under a temp home
// directory.
func writeCredentials(t *testing.T, body string) {
	t.Helper()
	home := setTempHome(t)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
}

func TestReadClaudeCodeOAuthToken(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "valid unexpired token",
			body: `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + future + `}}`,
			want: "tok-123",
		},
		{
			name: "no expiry field is accepted",
			body: `{"claudeAiOauth":{"accessToken":"tok-abc"}}`,
			want: "tok-abc",
		},
		{
			name:    "expired token rejected",
			body:    `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + past + `}}`,
			wantErr: true,
		},
		{
			name:    "empty token rejected",
			body:    `{"claudeAiOauth":{"accessToken":""}}`,
			wantErr: true,
		},
		{
			name:    "malformed json rejected",
			body:    `{not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeCredentials(t, tt.body)
			got, err := readClaudeCodeOAuthToken()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got token %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadClaudeCodeOAuthToken_MissingFile(t *testing.T) {
	setTempHome(t) // no .claude/.credentials.json
	if _, err := readClaudeCodeOAuthToken(); err == nil {
		t.Fatal("expected error for missing credentials file")
	}
}

func TestFetchUsageAPIWithAuth_OAuth(t *testing.T) {
	var gotAuth, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2999-01-01T00:00:00Z"},` +
			`"seven_day":{"utilization":13,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	usage, err := fetchUsageAPIWithAuth(context.Background(), srv.URL, oauthAuthHeaders("tok-xyz"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.Utilization != 42 {
		t.Fatalf("five_hour utilization = %+v, want 42", usage.FiveHour)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 13 {
		t.Fatalf("seven_day utilization = %+v, want 13", usage.SevenDay)
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-xyz")
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta header = %q, want %q", gotBeta, "oauth-2025-04-20")
	}
}

func TestFetchUsageAPIWithAuth_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	if _, err := fetchUsageAPIWithAuth(context.Background(), srv.URL, oauthAuthHeaders("tok")); err == nil || strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("expected sanitized 401 error, got %v", err)
	}
}

// TestCookieAuthHeaders verifies the cookie/org source's scheme: session
// cookies plus browser-shaped headers, and specifically that it never sets
// the OAuth scheme's Authorization header — the two auth schemes must stay
// distinct since fetchUsageAPIWithAuth applies exactly one of them per call.
func TestCookieAuthHeaders(t *testing.T) {
	cookies := map[string]string{
		"sessionKey":    "sess-abc",
		"lastActiveOrg": "org-123",
	}
	req := httptest.NewRequest(http.MethodGet, "https://claude.ai/api/organizations/org-123/usage", nil)
	cookieAuthHeaders(cookies)(req)

	cookieHeader := req.Header.Get("Cookie")
	for name, value := range cookies {
		want := name + "=" + value
		if !strings.Contains(cookieHeader, want) {
			t.Errorf("Cookie header %q missing %q", cookieHeader, want)
		}
	}
	if got := req.Header.Get("Referer"); got != "https://claude.ai/settings/usage" {
		t.Errorf("Referer = %q, want %q", got, "https://claude.ai/settings/usage")
	}
	if got := req.Header.Get("anthropic-client-platform"); got != "web_claude_ai" {
		t.Errorf("anthropic-client-platform = %q, want %q", got, "web_claude_ai")
	}
	if req.Header.Get("User-Agent") == "" {
		t.Error("User-Agent not set")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header should not be set on the cookie scheme, got %q", got)
	}
	if got := req.Header.Get("anthropic-beta"); got != "" {
		t.Errorf("anthropic-beta header should not be set on the cookie scheme, got %q", got)
	}
}

// TestOAuthAuthHeaders verifies the OAuth source's scheme: a bearer token
// plus the oauth-beta header, and that it never sets the cookie scheme's
// Cookie/Referer/platform headers.
func TestOAuthAuthHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	oauthAuthHeaders("tok-xyz")(req)

	if got := req.Header.Get("Authorization"); got != "Bearer tok-xyz" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-xyz")
	}
	if got := req.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q, want %q", got, "oauth-2025-04-20")
	}
	if got := req.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie header should not be set on the oauth scheme, got %q", got)
	}
	if got := req.Header.Get("Referer"); got != "" {
		t.Errorf("Referer header should not be set on the oauth scheme, got %q", got)
	}
	if got := req.Header.Get("anthropic-client-platform"); got != "" {
		t.Errorf("anthropic-client-platform header should not be set on the oauth scheme, got %q", got)
	}
}

// TestUsageAuthSources_NamesAndOrder pins down the fixture list itself:
// cookie/org is tried before oauth, matching the macOS-first, CLI-fallback
// priority documented on usageAuthSources.
func TestUsageAuthSources_NamesAndOrder(t *testing.T) {
	p := &Provider{}
	sources := p.usageAuthSources("org-uuid")
	if len(sources) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(sources))
	}
	if sources[0].name != "cookie" {
		t.Errorf("sources[0].name = %q, want %q", sources[0].name, "cookie")
	}
	if sources[1].name != "oauth" {
		t.Errorf("sources[1].name = %q, want %q", sources[1].name, "oauth")
	}
}

// TestUsageAuthSources_OAuthPrepare_UsesOAuthSchemeAndURL exercises the real
// "oauth" fixture's prepare() end to end: it should resolve to oauthUsageURL
// and a setAuth closure using the bearer scheme, never the cookie scheme.
func TestUsageAuthSources_OAuthPrepare_UsesOAuthSchemeAndURL(t *testing.T) {
	futureMs := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok-fixture","expiresAt":`+futureMs+`}}`)

	p := &Provider{}
	sources := p.usageAuthSources("org-uuid")
	src := sources[1]

	url, setAuth, err := src.prepare()
	if err != nil {
		t.Fatalf("prepare: unexpected error: %v", err)
	}
	if url != oauthUsageURL {
		t.Errorf("url = %q, want oauthUsageURL %q", url, oauthUsageURL)
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	setAuth(req)
	if got := req.Header.Get("Authorization"); got != "Bearer tok-fixture" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-fixture")
	}
	if got := req.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie header should not be set by the oauth fixture, got %q", got)
	}
}

func TestReadUsageAPIIsolationRotationAndBackoff(t *testing.T) {
	calls := 0
	status := http.StatusOK
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		authorization = r.Header.Get("Authorization")
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("secret response body"))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	old := oauthUsageURL
	oauthUsageURL = srv.URL
	defer func() { oauthUsageURL = old }()
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"first"}}`)
	setCacheIdentity(t, "user-a", "org-a")
	p := &Provider{}
	snapshot := func(account, user string) core.UsageSnapshot {
		s := core.NewUsageSnapshot("claude_code", account)
		s.Raw["account_uuid"] = user
		return s
	}
	a := snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &a); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, a.Attributes["usage_api_observed_at"]); err != nil {
		t.Fatalf("missing fresh observation: %v", err)
	}
	if calls != 1 || authorization != "Bearer first" {
		t.Fatalf("unexpected request: calls=%d auth=%q", calls, authorization)
	}
	b := snapshot("account-b", "user-b")
	if err := p.readUsageAPI(context.Background(), "org-a", &b); err == nil || calls != 1 || len(b.Metrics) != 0 {
		t.Fatal("mismatched identity must not use another account's credentials or quota")
	}
	throttled := snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &throttled); err == nil || calls != 1 || len(throttled.Metrics) != 0 || throttled.Attributes["usage_api_observed_at"] != "" {
		t.Fatal("throttled fetch must not claim a fresh cached observation")
	}
	key := "org-a\x00user-a"
	p.usageAttempts[key] = time.Now().Add(-time.Minute)
	creds := filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"second"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status = http.StatusTooManyRequests
	failed := snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &failed); err == nil || strings.Contains(err.Error(), "secret response body") || len(failed.Metrics) != 0 || failed.Attributes["usage_api_observed_at"] != "" {
		t.Fatalf("failed request must not produce quota or leak its body: %v", err)
	}
	if calls != 2 || authorization != "Bearer second" {
		t.Fatal("rotated credentials were not used")
	}
	again := snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &again); err == nil || calls != 2 {
		t.Fatal("failure must back off")
	}
}

func TestCookieQuotaRequiresVerifiedAccountAndOrganization(t *testing.T) {
	setTempHome(t)
	setCacheIdentity(t, "user-a", "org-a")
	body := `{"uuid":"user-a","email_address":"a@example.com","memberships":[{"organization":{"uuid":"org-a"}}]}`
	status := http.StatusOK
	usageCalls := 0
	var cookieSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieSeen = r.Header.Get("Cookie")
		if r.URL.Path == "/account" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		usageCalls++
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":31,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	oldURL := cookieAccountURL
	cookieAccountURL = srv.URL + "/account"
	defer func() { cookieAccountURL = oldURL }()
	p := &Provider{}
	fetch := func(cookie string) (core.UsageSnapshot, error) {
		s := core.NewUsageSnapshot("claude_code", "account-a")
		s.Raw["account_uuid"] = "user-a"
		err := p.tryUsageAuthSource(context.Background(), "cookie", srv.URL+"/usage", cookieAuthHeaders(map[string]string{"sessionKey": cookie}), "org-a", "a@example.com", &s)
		return s, err
	}
	s, err := fetch("first")
	if err != nil || usageCalls != 1 || s.Attributes["usage_api_observed_at"] == "" || !strings.Contains(cookieSeen, "first") {
		t.Fatalf("verified cookie fetch failed: %v", err)
	}
	cases := []struct {
		body   string
		status int
	}{
		{`{"uuid":"user-b","memberships":[{"organization":{"uuid":"org-a"}}]}`, http.StatusOK},
		{`{"uuid":"user-a","memberships":[{"organization":{"uuid":"org-b"}}]}`, http.StatusOK},
		{`{"memberships":[{"organization":{"uuid":"org-a"}}]}`, http.StatusOK},
		{`secret body`, http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		body, status = tc.body, tc.status
		s, err = fetch("rotated")
		if err == nil || strings.Contains(err.Error(), "secret body") || usageCalls != 1 || len(s.Metrics) != 0 || s.Attributes["usage_api_observed_at"] != "" {
			t.Fatalf("unverified cookie produced quota or leaked data: %v", err)
		}
	}
	body, status = `{"email_address":"A@example.com","memberships":[{"organization":{"uuid":"org-a"}}]}`, http.StatusOK
	s, err = fetch("rotated")
	if err != nil || usageCalls != 2 || !strings.Contains(cookieSeen, "rotated") || s.Attributes["usage_api_observed_at"] == "" {
		t.Fatalf("verified rotated cookie fetch failed: %v", err)
	}
}

func TestReadUsageAPIRetryAfterAndAccountIsolation(t *testing.T) {
	calls := 0
	status := http.StatusTooManyRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if status != http.StatusOK {
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "600")
			}
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	old := oauthUsageURL
	oauthUsageURL = srv.URL
	defer func() { oauthUsageURL = old }()
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok"}}`)
	setCacheIdentity(t, "user-a", "org-a")
	p := &Provider{}
	snapshot := func(account, user string) core.UsageSnapshot {
		s := core.NewUsageSnapshot("claude_code", account)
		s.Raw["account_uuid"] = user
		return s
	}
	a := snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &a); err == nil || calls != 1 || len(a.Metrics) != 0 {
		t.Fatalf("429 must fail without quota: %v", err)
	}
	key := "org-a\x00user-a"
	wait := time.Until(p.usageRetryAt[key])
	if wait < 9*time.Minute || wait > 10*time.Minute {
		t.Fatalf("backoff = %s, want about 10m", wait)
	}
	p.usageAttempts[key] = time.Now().Add(-time.Hour)
	a = snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &a); err == nil || calls != 1 || a.Attributes["usage_api_observed_at"] != "" {
		t.Fatal("retry-after must prevent requests without presenting cached success")
	}
	setCacheIdentity(t, "user-b", "org-b")
	status = http.StatusOK
	b := snapshot("account-b", "user-b")
	if err := p.readUsageAPI(context.Background(), "org-b", &b); err != nil || calls != 2 || b.Attributes["usage_api_observed_at"] == "" {
		t.Fatalf("another identity must not inherit backoff: %v", err)
	}
	setCacheIdentity(t, "user-a", "org-a")
	p.usageRetryAt[key] = time.Now().Add(-time.Second)
	status = http.StatusServiceUnavailable
	a = snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &a); err == nil || calls != 3 || p.usageThrottleFailures[key] != 2 {
		t.Fatalf("second throttle must increase backoff: %v", err)
	}
	wait = time.Until(p.usageRetryAt[key])
	if wait < 110*time.Second || wait > 2*time.Minute {
		t.Fatalf("second backoff = %s, want about 2m", wait)
	}
	p.usageRetryAt[key] = time.Now().Add(-time.Second)
	p.usageAttempts[key] = time.Now().Add(-time.Hour)
	status = http.StatusOK
	a = snapshot("account-a", "user-a")
	if err := p.readUsageAPI(context.Background(), "org-a", &a); err != nil || calls != 4 || p.usageThrottleFailures[key] != 0 {
		t.Fatalf("success must reset backoff: %v", err)
	}
}
