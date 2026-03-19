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
	if os.Getenv("LEDIT_TEST_BROWSER") == "" {
		t.Skip("skipping: set LEDIT_TEST_BROWSER=1 to run browser tests")
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
	if os.Getenv("LEDIT_TEST_BROWSER") == "" {
		t.Skip("skipping: set LEDIT_TEST_BROWSER=1 to run browser tests")
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
	if os.Getenv("LEDIT_TEST_BROWSER") == "" {
		t.Skip("skipping: set LEDIT_TEST_BROWSER=1 to run browser tests")
	}

	r := NewBrowserRenderer()
	r.Close()

	_, err := r.RenderPage(context.Background(), "https://example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}
