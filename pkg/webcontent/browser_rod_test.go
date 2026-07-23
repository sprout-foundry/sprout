//go:build browser

package webcontent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRodRenderer_RenderPage_Example(t *testing.T) {
	if os.Getenv("SPROUT_TEST_BROWSER") == "" {
		t.Skip("skipping: set SPROUT_TEST_BROWSER=1 to run browser tests")
	}

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := r.RenderPage(ctx, "https://example.com")
	require.NoError(t, err)
	assert.Contains(t, html, "Example Domain")
	// The rendered HTML should still contain script tags (from the source),
	// but the important thing is we got rendered content.
	assert.NotEmpty(t, html)
}

func TestRodRenderer_RenderPage_SPA(t *testing.T) {
	if os.Getenv("SPROUT_TEST_BROWSER") == "" {
		t.Skip("skipping: set SPROUT_TEST_BROWSER=1 to run browser tests")
	}

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// React docs site is a known SPA — raw HTML has minimal visible text.
	html, err := r.RenderPage(ctx, "https://react.dev")
	require.NoError(t, err)

	// After rendering, we should see substantial content (not an empty shell).
	assert.Greater(t, len(html), 1000, "SPA should produce rendered HTML content")

	// The rendered page should include meaningful text content.
	assert.Contains(t, html, "React", "rendered React docs should mention React")
}

func TestRodRenderer_CloseBeforeRender(t *testing.T) {
	if os.Getenv("SPROUT_TEST_BROWSER") == "" {
		t.Skip("skipping: set SPROUT_TEST_BROWSER=1 to run browser tests")
	}

	r := NewBrowserRenderer()
	r.Close()

	_, err := r.RenderPage(context.Background(), "https://example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestGetNavigationTimeout(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectedDelay time.Duration
	}{
		// Localhost URLs — 10s timeout
		{"http localhost with port", "http://localhost:8080/foo", localhostTimeout},
		{"http localhost plain", "http://localhost", localhostTimeout},
		{"http 127.0.0.1 with port", "http://127.0.0.1:3000/bar", localhostTimeout},
		{"http [::1] with port", "http://[::1]:9000/baz", localhostTimeout},
		{"https localhost with port", "https://localhost:8443/qux", localhostTimeout},
		{"https 127.0.0.1 with port", "https://127.0.0.1:443/test", localhostTimeout},
		{"https [::1] with port", "https://[::1]:8080/test", localhostTimeout},

		// Remote URLs — 30s timeout
		{"https remote", "https://example.com/page", remoteTimeout},
		{"http remote", "http://example.com", remoteTimeout},
		{"https github", "https://github.com/user/repo", remoteTimeout},
		{"https with path and query", "https://api.example.com/v1?key=value", remoteTimeout},

		// Edge cases
		{"localhost no port (http)", "http://localhost", localhostTimeout},
		{"localhost no port (https)", "https://localhost", localhostTimeout},
		{"127.0.0.1 no port (http)", "http://127.0.0.1", localhostTimeout},
		{"127.0.0.1 no port (https)", "https://127.0.0.1", localhostTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getNavigationTimeout(tt.url)
			assert.Equal(t, tt.expectedDelay, got, "url=%q", tt.url)
		})
	}
}
