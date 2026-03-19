package webcontent

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// isHTMLContent
// ---------------------------------------------------------------------------

func TestIsHTMLContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"text/html plain", "text/html", true},
		{"text/html with charset", "text/html; charset=utf-8", true},
		{"application/xhtml+xml", "application/xhtml+xml", true},
		{"application/json", "application/json", false},
		{"empty string", "", false},
		{"text/plain", "text/plain", false},
	{"application/octet-stream", "application/octet-stream", false},
		{"TEXT/HTML uppercase", "TEXT/HTML", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isHTMLContent(tc.contentType))
		})
	}
}

// ---------------------------------------------------------------------------
// wrapWithBanner
// ---------------------------------------------------------------------------

func TestWrapWithBanner_BasicURLAndContent(t *testing.T) {
	const (
		testURL     = "https://example.com/page"
		testContent = "Hello world"
	)
	result := wrapWithBanner(testURL, testContent)

	assert.Contains(t, result, fmt.Sprintf("Content from URL: %s", testURL))
	assert.Contains(t, result, testContent)
	assert.Contains(t, result, fmt.Sprintf("End of content from URL: %s", testURL))
	assert.Contains(t, result, "Hello world")
}

func TestWrapWithBanner_EmptyContent(t *testing.T) {
	result := wrapWithBanner("https://example.com", "")
	assert.Contains(t, result, "Content from URL: https://example.com")
	assert.Contains(t, result, "End of content from URL: https://example.com")
}

func TestWrapWithBanner_ContainsNewlines(t *testing.T) {
	result := wrapWithBanner("https://example.com", "body text")
	// Banner starts with a newline before the marker
	assert.True(t, strings.HasPrefix(result, "\n---"))
	// Ends with a newline
	assert.True(t, strings.HasSuffix(result, "---\n"))
}

// ---------------------------------------------------------------------------
// truncateContent
// ---------------------------------------------------------------------------

func TestTruncateContent_UnderLimit(t *testing.T) {
	w := NewWebContentFetcher()
	content := strings.Repeat("a", 100)
	result, err := w.truncateContent(content)
	assert.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestTruncateContent_ExactlyAtLimit(t *testing.T) {
	w := NewWebContentFetcher()
	// Build content exactly maxContentSize bytes
	content := strings.Repeat("a", maxContentSize)
	result, err := w.truncateContent(content)
	assert.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestTruncateContent_OverLimit(t *testing.T) {
	w := NewWebContentFetcher()
	// 1 byte over the limit
	content := strings.Repeat("a", maxContentSize+1)
	result, err := w.truncateContent(content)

	assert.NoError(t, err)
	// Should contain the truncation suffix
	assert.Contains(t, result, truncatedSuffix)
	// Should contain the size info
	assert.Contains(t, result, "original: 1.0 MB")
	// The result should be longer than maxContentSize due to the suffix, but
	// the original content portion should be at most maxContentSize bytes.
	assert.LessOrEqual(t, strings.Index(result, truncatedSuffix), maxContentSize)
}

func TestTruncateContent_UTF8SafeAtBoundary(t *testing.T) {
	w := NewWebContentFetcher()

	// Build content where a multi-byte rune lands right at the truncation boundary.
	// '日' is 3 bytes in UTF-8. Fill up to (maxContentSize - 1) bytes with ASCII,
	// then add the multi-byte rune so the cut falls inside it.
	oneRune := "日"
	prefixLen := maxContentSize - 1
	prefix := strings.Repeat("x", prefixLen)
	content := prefix + oneRune

	result, err := w.truncateContent(content)

	assert.NoError(t, err)
	assert.Contains(t, result, truncatedSuffix)

	// The original content portion (before the suffix) must be valid UTF-8.
	suffixIdx := strings.Index(result, "\n\n[CONTENT TRUNCATED")
	assert.Greater(t, suffixIdx, 0, "suffix should be present")

	originalPortion := result[:suffixIdx]
	assert.True(t, utf8.ValidString(originalPortion), "truncated content must be valid UTF-8")
	// Should be exactly prefixLen bytes (the multi-byte char was dropped because
	// boundary was right before its last byte).
	assert.Equal(t, prefixLen, len(originalPortion),
		"should truncate before incomplete rune")
}
