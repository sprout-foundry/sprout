package webcontent

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
