package webcontent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLocalhostURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:8080/app", true},
		{"http://localhost", true},
		{"https://localhost", true},
		{"https://localhost:443", true},
		{"http://example.com", false},
		{"https://google.com", false},
		{"http://127.0.0.1:3000", true},
		{"https://127.0.0.1:8080/app", true},
		{"http://[::1]:3000", true},
		{"https://[::1]", true},
		{"", false},
		{"localhost:3000", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalhostURL(tt.url))
		})
	}
}

func TestLocalhostOrSPA(t *testing.T) {
	assert.Equal(t, "localhost URL (JS likely needed)", localhostOrSPA("http://localhost:3000"))
	assert.Equal(t, "localhost URL (JS likely needed)", localhostOrSPA("https://localhost"))
	assert.Equal(t, "localhost URL (JS likely needed)", localhostOrSPA("http://127.0.0.1:8080"))
	assert.Equal(t, "localhost URL (JS likely needed)", localhostOrSPA("https://127.0.0.1"))
	assert.Equal(t, "localhost URL (JS likely needed)", localhostOrSPA("http://[::1]:3000"))
	assert.Equal(t, "SPA shell detected", localhostOrSPA("https://react.dev"))
	assert.Equal(t, "SPA shell detected", localhostOrSPA("https://example.com"))
}

func TestBrowseURL_EmptyURL(t *testing.T) {
	_, err := BrowseURL("", BrowseOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL cannot be empty")
}

func TestBrowseURL_InvalidAction(t *testing.T) {
	_, err := BrowseURL("http://example.com", BrowseOptions{Action: "fly"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestBrowseURL_InvalidScheme(t *testing.T) {
	rejectCases := []string{
		"file:///etc/passwd",
		"FILE:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,<h1>hi</h1>",
		"ftp://files.example.com",
		"httpx://evil.com",
		"https:notascheme",
		"no-scheme-at-all",
	}
	for _, u := range rejectCases {
		t.Run(u, func(t *testing.T) {
			_, err := BrowseURL(u, BrowseOptions{})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "must start with http:// or https://")
		})
	}
	// Case-insensitive acceptance — these will fail at the nop renderer
	// but should NOT fail at the scheme check.
	for _, u := range []string{"HTTP://example.com", "HtTpS://example.com"} {
		t.Run("accept_"+u, func(t *testing.T) {
			_, err := BrowseURL(u, BrowseOptions{})
			// Should NOT be a scheme error; it will be a browser error instead
			if err != nil {
				assert.NotContains(t, err.Error(), "must start with http:// or https://")
			}
		})
	}
}

func TestBrowseURL_ScreenshotRequiresPath(t *testing.T) {
	_, err := BrowseURL("http://example.com", BrowseOptions{Action: "screenshot"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "screenshot_path is required")
}

type fakeBrowserRenderer struct {
	lastRunURL  string
	lastRunOpts BrowseOptions
	runResult   *BrowseResult
}

func (f *fakeBrowserRenderer) RenderPage(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeBrowserRenderer) Screenshot(context.Context, string, string, int, int, string) error {
	return nil
}

func (f *fakeBrowserRenderer) CaptureDOM(context.Context, string, int, int, string) (string, error) {
	return "<html><body>dom</body></html>", nil
}

func (f *fakeBrowserRenderer) Run(_ context.Context, url string, opts BrowseOptions) (*BrowseResult, error) {
	f.lastRunURL = url
	f.lastRunOpts = opts
	if f.runResult != nil {
		return f.runResult, nil
	}
	return &BrowseResult{FinalURL: url, VisibleText: "rendered text"}, nil
}

func (f *fakeBrowserRenderer) Close() {}

func withFakeGlobalBrowser(t *testing.T, renderer BrowserRenderer) {
	t.Helper()
	previousBrowser := globalBrowser
	previousOnce := globalBrowserOnce
	globalBrowser = renderer
	globalBrowserOnce = sync.Once{}
	globalBrowserOnce.Do(func() {
		globalBrowser = renderer
	})
	t.Cleanup(func() {
		globalBrowser = previousBrowser
		globalBrowserOnce = previousOnce
	})
}

func TestBrowseURL_InspectActionRunsInteractiveBrowserFlow(t *testing.T) {
	fake := &fakeBrowserRenderer{
		runResult: &BrowseResult{
			FinalURL:        "http://example.com/final",
			Title:           "Example",
			VisibleText:     "Hello world",
			ConsoleMessages: []string{"[log] ready"},
			Actions:         []string{"wait_for #app", "click #launch"},
		},
	}
	withFakeGlobalBrowser(t, fake)

	raw, err := BrowseURL("http://example.com", BrowseOptions{
		Action:          "inspect",
		WaitForSelector: "#app",
		Steps:           []BrowseStep{{Action: "click", Selector: "#launch"}},
		CaptureSelectors: []string{
			"#app",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "http://example.com", fake.lastRunURL)
	assert.Equal(t, "#app", fake.lastRunOpts.WaitForSelector)
	assert.Len(t, fake.lastRunOpts.Steps, 1)
	assert.Contains(t, fake.lastRunOpts.CaptureSelectors, "#app")
	assert.True(t, fake.lastRunOpts.IncludeConsole, "inspect mode should include browser diagnostics")
	assert.True(t, fake.lastRunOpts.CaptureNetwork, "inspect mode should capture network by default")
	assert.True(t, fake.lastRunOpts.CaptureStorage, "inspect mode should capture storage by default")
	assert.True(t, fake.lastRunOpts.CaptureCookies, "inspect mode should capture cookies by default")

	var result BrowseResult
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	assert.Equal(t, "http://example.com/final", result.FinalURL)
	assert.Equal(t, "Example", result.Title)
	assert.Contains(t, result.ConsoleMessages, "[log] ready")
}

func TestBrowseURL_TextActionUsesInteractiveFlowWhenAdvancedOptionsPresent(t *testing.T) {
	fake := &fakeBrowserRenderer{
		runResult: &BrowseResult{
			FinalURL:    "http://example.com",
			VisibleText: "hydrated text",
		},
	}
	withFakeGlobalBrowser(t, fake)

	text, err := BrowseURL("http://example.com", BrowseOptions{
		Action:          "text",
		WaitForSelector: "#app",
	})
	require.NoError(t, err)
	assert.Equal(t, "hydrated text", text)
	assert.True(t, fake.lastRunOpts.CaptureText)
}

func TestBrowseURL_InspectActionPreservesExtendedDebuggingOptions(t *testing.T) {
	fake := &fakeBrowserRenderer{
		runResult: &BrowseResult{
			SessionID:      "browser_123",
			FinalURL:       "http://example.com/settings",
			ReadyState:     "complete",
			Cookies:        map[string]string{"session": "abc"},
			LocalStorage:   map[string]string{"theme": "dark"},
			SessionStorage: map[string]string{"draft": "1"},
			NetworkRequests: []NetworkRequest{
				{Type: "fetch", URL: "/api/settings", Method: "GET", Status: 200, OK: true},
			},
		},
	}
	withFakeGlobalBrowser(t, fake)

	raw, err := BrowseURL("http://example.com", BrowseOptions{
		Action:           "inspect",
		WaitForSelector:  "#app",
		SessionID:        "browser_123",
		PersistSession:   true,
		ResponseMaxChars: 512,
		Steps: []BrowseStep{
			{Action: "navigate", Value: "http://example.com/settings"},
			{Action: "wait_for_text", Selector: "body", Expect: "Settings"},
			{Action: "assert_selector", Selector: "#save", Expect: "Save"},
			{Action: "assert_title", Expect: "Settings"},
			{Action: "assert_url", Expect: "/settings"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 512, fake.lastRunOpts.ResponseMaxChars)
	assert.Equal(t, "browser_123", fake.lastRunOpts.SessionID)
	assert.True(t, fake.lastRunOpts.PersistSession)
	require.Len(t, fake.lastRunOpts.Steps, 5)
	assert.Equal(t, "navigate", fake.lastRunOpts.Steps[0].Action)
	assert.Equal(t, "Settings", fake.lastRunOpts.Steps[1].Expect)
	assert.Equal(t, "Save", fake.lastRunOpts.Steps[2].Expect)
	assert.Equal(t, "Settings", fake.lastRunOpts.Steps[3].Expect)
	assert.Equal(t, "/settings", fake.lastRunOpts.Steps[4].Expect)

	var result BrowseResult
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	assert.Equal(t, "browser_123", result.SessionID)
	assert.Equal(t, "complete", result.ReadyState)
	assert.Equal(t, "abc", result.Cookies["session"])
	assert.Equal(t, "dark", result.LocalStorage["theme"])
	assert.Equal(t, "1", result.SessionStorage["draft"])
	require.Len(t, result.NetworkRequests, 1)
	assert.Equal(t, "/api/settings", result.NetworkRequests[0].URL)
}
