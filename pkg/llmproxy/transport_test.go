package llmproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// captureTransport is a test double that records the request it would have
// sent without actually performing any network I/O. Lets us assert on URL
// rewriting without spinning up an HTTPS server with valid TLS.
type captureTransport struct {
	mu      sync.Mutex
	lastReq *http.Request
	resp    *http.Response
	respErr error
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.lastReq = r.Clone(r.Context())
	c.mu.Unlock()
	if c.respErr != nil {
		return nil, c.respErr
	}
	if c.resp != nil {
		return c.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     http.Header{},
		Request:    r,
	}, nil
}

func newRewriteWithCapture(t *testing.T) (*rewriteTransport, *captureTransport) {
	t.Helper()
	cap := &captureTransport{}
	rt := &rewriteTransport{base: cap}
	return rt, cap
}

func TestRoundTrip_NoPlatformEndpoint_PassesThrough(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)

	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if cap.lastReq.URL.String() != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("URL should pass through unchanged when platform endpoint is unset; got %s", cap.lastReq.URL)
	}
}

func TestRoundTrip_RewritesKnownProviders(t *testing.T) {
	cases := []struct {
		name       string
		platform   string
		inputURL   string
		expectURL  string
		expectHost string
	}{
		{
			name:       "openai chat",
			platform:   "https://platform.example.com",
			inputURL:   "https://api.openai.com/v1/chat/completions",
			expectURL:  "https://platform.example.com/api/proxy/llm/openai/v1/chat/completions",
			expectHost: "platform.example.com",
		},
		{
			name:       "anthropic messages",
			platform:   "https://platform.example.com",
			inputURL:   "https://api.anthropic.com/v1/messages",
			expectURL:  "https://platform.example.com/api/proxy/llm/anthropic/v1/messages",
			expectHost: "platform.example.com",
		},
		{
			name:       "openrouter strips /api prefix",
			platform:   "https://platform.example.com",
			inputURL:   "https://openrouter.ai/api/v1/chat/completions",
			expectURL:  "https://platform.example.com/api/proxy/llm/openrouter/v1/chat/completions",
			expectHost: "platform.example.com",
		},
		{
			name:       "deepinfra preserves nested path",
			platform:   "https://platform.example.com",
			inputURL:   "https://api.deepinfra.com/v1/openai/chat/completions",
			expectURL:  "https://platform.example.com/api/proxy/llm/deepinfra/v1/openai/chat/completions",
			expectHost: "platform.example.com",
		},
		{
			name:       "preserves query string",
			platform:   "https://platform.example.com",
			inputURL:   "https://api.openai.com/v1/models?limit=10",
			expectURL:  "https://platform.example.com/api/proxy/llm/openai/v1/models?limit=10",
			expectHost: "platform.example.com",
		},
		{
			name:       "platform URL with trailing slash is normalized",
			platform:   "https://platform.example.com/",
			inputURL:   "https://api.openai.com/v1/models",
			expectURL:  "https://platform.example.com/api/proxy/llm/openai/v1/models",
			expectHost: "platform.example.com",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt, cap := newRewriteWithCapture(t)
			rt.platformBase.Store(strings.TrimRight(c.platform, "/"))

			req, _ := http.NewRequest("POST", c.inputURL, nil)
			if _, err := rt.RoundTrip(req); err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}

			if got := cap.lastReq.URL.String(); got != c.expectURL {
				t.Errorf("URL = %q, want %q", got, c.expectURL)
			}
			if cap.lastReq.Host != c.expectHost {
				t.Errorf("Host = %q, want %q", cap.lastReq.Host, c.expectHost)
			}
		})
	}
}

func TestRoundTrip_UnknownProviderPassesThrough(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	rt.platformBase.Store("https://platform.example.com")

	req, _ := http.NewRequest("GET", "https://api.example.com/v1/anything", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if cap.lastReq.URL.String() != "https://api.example.com/v1/anything" {
		t.Errorf("unknown provider should not be rewritten; got %s", cap.lastReq.URL)
	}
}

func TestRoundTrip_DoesNotMutateInputRequest(t *testing.T) {
	rt, _ := newRewriteWithCapture(t)
	rt.platformBase.Store("https://platform.example.com")

	original, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	originalURL := original.URL.String()

	if _, err := rt.RoundTrip(original); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if original.URL.String() != originalURL {
		t.Errorf("input request was mutated: URL became %s", original.URL)
	}
}

// TestInstall_Idempotent ensures repeated installs don't build a nested
// chain of rewriteTransports — each layer would double the work on every
// request, which would silently degrade perf as the page reloads accumulate.
func TestInstall_Idempotent(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	Install()
	firstInstall, ok := http.DefaultTransport.(*rewriteTransport)
	if !ok {
		t.Fatal("Install did not replace http.DefaultTransport with rewriteTransport")
	}

	// Second install should be a no-op — same instance.
	Install()
	secondInstall, ok := http.DefaultTransport.(*rewriteTransport)
	if !ok {
		t.Fatal("Install lost the rewriteTransport on second call")
	}
	if firstInstall != secondInstall {
		t.Error("Install built a new wrapper instead of being idempotent")
	}
}

func TestSetPlatformEndpoint_Concurrent(t *testing.T) {
	// SetPlatformEndpoint must be safe to call from any goroutine.
	// In WASM we expect it to only fire from the JS event-loop thread,
	// but this catches accidental misuse and pins atomic semantics.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			SetPlatformEndpoint("https://platform.example.com")
			_ = GetPlatformEndpoint()
		}(i)
	}
	wg.Wait()
	if GetPlatformEndpoint() != "https://platform.example.com" {
		t.Errorf("unexpected final value: %q", GetPlatformEndpoint())
	}
	SetPlatformEndpoint("") // reset for other tests
}

// TestInstall_IntegrationWithHTTPClient is an end-to-end check that
// installing the transport actually routes through the rewriter for the
// default http.Client. The test runs a mock platform server, points
// the rewriter at it, and verifies an OpenAI-style request lands on the
// mock server's /api/proxy/llm/openai/* path.
func TestInstall_IntegrationWithHTTPClient(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
		SetPlatformEndpoint("")
	})

	var receivedPath string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer mock.Close()

	Install()
	SetPlatformEndpoint(mock.URL)

	// Use a plain http.Client without overriding Transport — it should
	// pick up the installed DefaultTransport and route through the mock.
	resp, err := http.Get("https://api.openai.com/v1/models")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if receivedPath != "/api/proxy/llm/openai/v1/models" {
		t.Errorf("mock received path = %q, want /api/proxy/llm/openai/v1/models", receivedPath)
	}
}

func TestMatchProvider_AllRegistered(t *testing.T) {
	// Smoke test: every registered provider must round-trip a sample URL
	// to itself + a non-empty suffix. Catches obvious typos in the
	// providers table (host case, prefix direction, etc.).
	samples := map[string]string{
		"openai":     "https://api.openai.com/v1/models",
		"anthropic":  "https://api.anthropic.com/v1/messages",
		"openrouter": "https://openrouter.ai/api/v1/chat/completions",
		"deepinfra":  "https://api.deepinfra.com/v1/openai/chat/completions",
		"mistral":    "https://api.mistral.ai/v1/chat/completions",
		"cerebras":   "https://api.cerebras.ai/v1/models",
		"groq":       "https://api.groq.com/openai/v1/models",
		"together":   "https://api.together.xyz/v1/models",
	}
	seen := map[string]bool{}
	for wantProvider, raw := range samples {
		u, _ := url.Parse(raw)
		got, suffix, ok := matchProvider(u)
		if !ok {
			t.Errorf("%s: matchProvider returned ok=false for %s", wantProvider, raw)
			continue
		}
		if got != wantProvider {
			t.Errorf("%s: matchProvider returned provider=%q, want %q", wantProvider, got, wantProvider)
		}
		if suffix == "" || !strings.HasPrefix(suffix, "/") {
			t.Errorf("%s: suffix should start with /, got %q", wantProvider, suffix)
		}
		seen[got] = true
	}
	// Sanity check: every registered provider should be sampled (catches
	// a new entry in providers.go that the tests don't cover).
	for _, p := range knownProviders {
		if !seen[p.provider] {
			t.Errorf("provider %q in registry but not covered by samples table", p.provider)
		}
	}
}

func TestRoundTrip_CorsProxy_RewritesURL(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	rt.corsProxy.Store("https://cors-proxy.example.com")

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	expected := "https://cors-proxy.example.com/https%3A%2F%2Fapi.openai.com%2Fv1%2Fchat%2Fcompletions"
	if got := cap.lastReq.URL.String(); got != expected {
		t.Errorf("URL = %q, want %q", got, expected)
	}
	if cap.lastReq.Host != "cors-proxy.example.com" {
		t.Errorf("Host = %q, want cors-proxy.example.com", cap.lastReq.Host)
	}
}

func TestRoundTrip_CorsProxy_RewritesAllURLs(t *testing.T) {
	cases := []struct {
		name      string
		inputURL  string
		expectURL string
	}{
		{
			name:      "openai",
			inputURL:  "https://api.openai.com/v1/chat/completions",
			expectURL: "https://cors-proxy.example.com/https%3A%2F%2Fapi.openai.com%2Fv1%2Fchat%2Fcompletions",
		},
		{
			name:      "anthropic",
			inputURL:  "https://api.anthropic.com/v1/messages",
			expectURL: "https://cors-proxy.example.com/https%3A%2F%2Fapi.anthropic.com%2Fv1%2Fmessages",
		},
		{
			name:      "openrouter with path prefix",
			inputURL:  "https://openrouter.ai/api/v1/chat/completions",
			expectURL: "https://cors-proxy.example.com/https%3A%2F%2Fopenrouter.ai%2Fapi%2Fv1%2Fchat%2Fcompletions",
		},
		{
			name:      "unknown provider still rewritten",
			inputURL:  "https://custom-llm.example.com/v1/chat",
			expectURL: "https://cors-proxy.example.com/https%3A%2F%2Fcustom-llm.example.com%2Fv1%2Fchat",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt, cap := newRewriteWithCapture(t)
			rt.corsProxy.Store("https://cors-proxy.example.com")

			req, _ := http.NewRequest("POST", c.inputURL, nil)
			if _, err := rt.RoundTrip(req); err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}
			if got := cap.lastReq.URL.String(); got != c.expectURL {
				t.Errorf("URL = %q, want %q", got, c.expectURL)
			}
		})
	}
}

func TestRoundTrip_CorsProxy_PriorityOverPlatformEndpoint(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	rt.platformBase.Store("https://platform.example.com")
	rt.corsProxy.Store("https://cors-proxy.example.com")

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// Should go through CORS proxy, NOT platform endpoint
	expected := "https://cors-proxy.example.com/https%3A%2F%2Fapi.openai.com%2Fv1%2Fchat%2Fcompletions"
	if got := cap.lastReq.URL.String(); got != expected {
		t.Errorf("URL = %q, want %q (cors proxy should take priority over platform endpoint)", got, expected)
	}
	// Verify it did NOT route through the platform proxy
	if strings.Contains(cap.lastReq.URL.String(), "platform.example.com") {
		t.Error("URL should NOT contain platform endpoint when cors proxy is set")
	}
}

func TestRoundTrip_CorsProxy_Disabled_PassesThrough(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	// corsProxy not set (empty string)

	req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if got := cap.lastReq.URL.String(); got != "https://api.openai.com/v1/models" {
		t.Errorf("URL = %q, want unchanged when cors proxy is disabled", got)
	}
}

func TestRoundTrip_CorsProxy_HTTPRequest(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	rt.corsProxy.Store("https://cors-proxy.example.com")

	req, _ := http.NewRequest("GET", "http://localhost:8080/v1/chat", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	expected := "https://cors-proxy.example.com/http%3A%2F%2Flocalhost%3A8080%2Fv1%2Fchat"
	if got := cap.lastReq.URL.String(); got != expected {
		t.Errorf("URL = %q, want %q", got, expected)
	}
}

func TestRoundTrip_CorsProxy_NonHTTP_PassesThrough(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	rt.corsProxy.Store("https://cors-proxy.example.com")

	req, _ := http.NewRequest("GET", "ws://localhost:8080/ws", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if got := cap.lastReq.URL.String(); got != "ws://localhost:8080/ws" {
		t.Errorf("URL = %q, want unchanged for non-HTTP(S) URLs", got)
	}
}

func TestRoundTrip_CorsProxy_TrailingSlashNormalized(t *testing.T) {
	rt, cap := newRewriteWithCapture(t)
	// SetCorsProxy strips trailing slashes; mimic that here since we
	// store directly on the transport.
	rt.corsProxy.Store(strings.TrimRight("https://cors-proxy.example.com/", "/"))

	req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	expected := "https://cors-proxy.example.com/https%3A%2F%2Fapi.openai.com%2Fv1%2Fmodels"
	if got := cap.lastReq.URL.String(); got != expected {
		t.Errorf("URL = %q, want %q (trailing slash should be normalized)", got, expected)
	}
}

func TestSetCorsProxy_GetCorsProxy_RoundTrip(t *testing.T) {
	SetCorsProxy("")
	defer SetCorsProxy("")

	if got := GetCorsProxy(); got != "" {
		t.Errorf("initial value = %q, want empty", got)
	}

	SetCorsProxy("https://cors-proxy.example.com")
	if got := GetCorsProxy(); got != "https://cors-proxy.example.com" {
		t.Errorf("GetCorsProxy = %q, want https://cors-proxy.example.com", got)
	}

	SetCorsProxy("https://cors-proxy.example.com/")
	if got := GetCorsProxy(); got != "https://cors-proxy.example.com" {
		t.Errorf("GetCorsProxy after trailing slash = %q, want trailing slash stripped", got)
	}

	SetCorsProxy("")
	if got := GetCorsProxy(); got != "" {
		t.Errorf("GetCorsProxy after clear = %q, want empty", got)
	}
}

func TestSetCorsProxy_Concurrent(t *testing.T) {
	SetCorsProxy("")
	defer SetCorsProxy("")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			SetCorsProxy("https://cors-proxy.example.com")
			_ = GetCorsProxy()
		}(i)
	}
	wg.Wait()
	if GetCorsProxy() != "https://cors-proxy.example.com" {
		t.Errorf("unexpected final value: %q", GetCorsProxy())
	}
}

func TestSetCorsProxy_RejectsInvalidURLs(t *testing.T) {
	SetCorsProxy("")
	defer SetCorsProxy("")

	cases := []struct {
		name  string
		input string
	}{
		{"ftp scheme", "ftp://proxy.example.com"},
		{"javascript scheme", "javascript:alert(1)"},
		{"random string", "not-a-url"},
		{"just hostname", "proxy.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			SetCorsProxy(c.input)
			if got := GetCorsProxy(); got != "" {
				t.Errorf("SetCorsProxy(%q) stored %q, want rejected (empty)", c.input, got)
			}
		})
	}

	// Verify valid URLs are accepted
	SetCorsProxy("https://cors-proxy.example.com")
	if got := GetCorsProxy(); got != "https://cors-proxy.example.com" {
		t.Errorf("valid https URL not stored: got %q", got)
	}
	SetCorsProxy("")
}
