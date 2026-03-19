package webcontent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Evaluation: fetch pipeline integration tests (no network calls)
// ---------------------------------------------------------------------------

// EVAL-FP-1: Verify the full pipeline doesn't break when NeedsRendering
// returns true but browser is nop (the common default-build path).
func TestFetchPipeline_SPADetectedBrowserNop_FallsBack(t *testing.T) {
	// This tests the code path in fetchDirectURL:
	//   NeedsRendering(html) == true → try browser → nop returns error → fall back to HTMLToText(raw)
	spaShell := `<html><body><div id="root"></div><script src="/static/js/main.js"></script></body></html>`
	assert.True(t, NeedsRendering(spaShell), "precondition: SPA shell should be detected")

	// Simulate what fetchDirectURL does:
	text := HTMLToText(spaShell)
	// Should produce something (likely empty but not panic)
	t.Logf("HTMLToText on SPA shell: %q", text)
}

// EVAL-FP-2: Verify the full pipeline on a content-heavy page (most common case).
func TestFetchPipeline_ContentPage_NoRenderingNeeded(t *testing.T) {
	contentPage := `<html><head><title>Blog</title></head><body>
<h1>My Blog</h1>
<p>This is a blog post about travel with enough text to be a real page.</p>
<p>I visited Japan last summer and it was wonderful.</p>
</body></html>`

	assert.False(t, NeedsRendering(contentPage), "content page should not need rendering")
	text := HTMLToText(contentPage)
	assert.Contains(t, text, "My Blog")
	assert.Contains(t, text, "travel")
}

// EVAL-FP-3: Verify the pipeline on a page that's technically HTML but
// contains no useful body content (just a title and empty body).
func TestFetchPipeline_HTMLWithOnlyTitle_Useful(t *testing.T) {
	html := `<html><head><title>Page Title Here</title><meta name="description" content="A useful description"></head><body></body></html>`
	text := HTMLToText(html)
	// Even though body is empty, head metadata should appear
	assert.Contains(t, text, "Title: Page Title Here")
	assert.Contains(t, text, "Description: A useful description")
	t.Logf("Output:\n%s", text)
}

// EVAL-FP-4: Stress test — verify no panic on various malformed inputs.
func TestNeedsRendering_MalformedInputs_NoPanic(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{"empty string", ""},
		{"just text", "Hello world"},
		{"partial tag", "<div id=\"root\">"},
		{"partial close tag", "</div>"},
		{"only script open", "<script>var x = 1;"},
		{"nested unclosed tags", "<div><div><div>text"},
		{"backwards tags", "><div><html"},
		{"null bytes", "<html\x00<body>text</body></html>"},
		{"unicode", "<html><body>日本語テスト<body></html>"},
		{"huge whitespace", strings.Repeat(" \t\n\r\f", 10000)},
		{"bare DOCTYPE", "<!DOCTYPE html>"},
		{"comment only", "<!-- just a comment -->"},
		{"many nested comments", "<!-----><!-- --><!----->"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() { NeedsRendering(tc.html) })
		})
	}
}

// EVAL-FP-5: Benchmark of NeedsRendering on various sizes.
func BenchmarkNeedsRendering_Small(b *testing.B) {
	html := strings.Repeat(`<p>Hello world this is a test paragraph.</p>`, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NeedsRendering(html)
	}
}

func BenchmarkNeedsRendering_Medium(b *testing.B) {
	html := strings.Repeat(`<p>Hello world this is a test paragraph.</p>`, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NeedsRendering(html)
	}
}

func BenchmarkNeedsRendering_Large(b *testing.B) {
	html := strings.Repeat(`<p>Hello world this is a test paragraph.</p>`, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NeedsRendering(html)
	}
}

func BenchmarkNeedsRendering_SPAShell(b *testing.B) {
	html := `<html><body><div id="root"></div><script src="/static/js/main.js"></script></body></html>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NeedsRendering(html)
	}
}

func BenchmarkHTMLToText_SmallPage(b *testing.B) {
	html := `<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HTMLToText(html)
	}
}

func BenchmarkHTMLToText_MediumPage(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<html><head><title>Blog</title></head><body>`)
	for i := 0; i < 50; i++ {
		sb.WriteString(fmt.Sprintf("<h2>Section %d</h2><p>Content paragraph %d with enough text to be realistic.</p>", i, i))
	}
	sb.WriteString(`</body></html>`)
	html := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HTMLToText(html)
	}
}

// EVAL-FP-6: Compute real-world end-to-end timing for a large page.
func TestNeedsRendering_RealWorldTiming(t *testing.T) {
	// Simulates a 10KB HTML page (webpage + scripts typical for small sites)
	var sb strings.Builder
	sb.WriteString(`<html><head><title>Restaurant Page</title><meta name="description" content="Great food">`)
	sb.WriteString(`<script src="/js/analytics.js"></script>`)
	sb.WriteString(`<script src="/js/lazysizes.min.js"></script>`)
	sb.WriteString(`<link rel="stylesheet" href="/css/style.css">`)
	sb.WriteString(`</head><body><nav><a href="/">Home</a><a href="/about">About</a><a href="/menu">Menu</a><a href="/contact">Contact</a></nav>`)
	for i := 0; i < 20; i++ {
		sb.WriteString(fmt.Sprintf(`<p>Paragraph %d with realistic content about the restaurant. We serve great food and drinks every day of the week.</p>`, i))
	}
	sb.WriteString(`<script>ga('create','UA-XXXXX-Y','auto');</script>`)
	sb.WriteString(`</body></html>`)
	html := sb.String()

	t.Logf("Page size: %d bytes", len(html))

	start := time.Now()
	result := NeedsRendering(html)
	elapsed := time.Since(start)

	t.Logf("NeedsRendering=%v in %v", result, elapsed)
	assert.False(t, result, "normal restaurant page should not need rendering")
	assert.Less(t, elapsed, 10*time.Millisecond, "should complete in under 10ms")
}
