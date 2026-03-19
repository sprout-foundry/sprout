package webcontent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// HTMLToText
// ---------------------------------------------------------------------------

func TestHTMLToText_BasicParagraphExtraction(t *testing.T) {
	result := HTMLToText(`<p>Hello world</p>`)
	assert.Contains(t, result, "Hello world")
}

func TestHTMLToText_NestedElements(t *testing.T) {
	result := HTMLToText(`<div><p>Hello</p><p>World</p></div>`)
	assert.Contains(t, result, "Hello")
	assert.Contains(t, result, "World")
}

func TestHTMLToText_ScriptAndStyleStripping(t *testing.T) {
	input := `<html><head><style>body{color:red}</style></head><body><p>Hello</p><script>alert(1)</script></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Hello")
	assert.NotContains(t, result, "color:red")
	assert.NotContains(t, result, "alert")
}

func TestHTMLToText_LinkWithHref(t *testing.T) {
	result := HTMLToText(`<a href="https://example.com">Click here</a>`)
	assert.Contains(t, result, "Click here (https://example.com)")
}

func TestHTMLToText_LinkWithoutHref(t *testing.T) {
	result := HTMLToText(`<a>Click here</a>`)
	assert.Contains(t, result, "Click here")
	assert.NotContains(t, result, "(https://")
}

func TestHTMLToText_LinkWithEmptyText(t *testing.T) {
	result := HTMLToText(`<a href="https://example.com"></a>`)
	assert.NotContains(t, result, "(https://example.com)")
}

func TestHTMLToText_LinkWithNoHrefAttribute(t *testing.T) {
	result := HTMLToText(`<a>No href</a>`)
	assert.Contains(t, result, "No href")
	assert.NotContains(t, result, "(")
}

func TestHTMLToText_OrderedList(t *testing.T) {
	result := HTMLToText(`<ol><li>One</li><li>Two</li><li>Three</li></ol>`)
	assert.Contains(t, result, "1. One")
	assert.Contains(t, result, "2. Two")
	assert.Contains(t, result, "3. Three")
}

func TestHTMLToText_UnorderedList(t *testing.T) {
	result := HTMLToText(`<ul><li>A</li><li>B</li></ul>`)
	assert.Contains(t, result, "- A")
	assert.Contains(t, result, "- B")
}

func TestHTMLToText_NestedLists(t *testing.T) {
	result := HTMLToText(`<ol><li>First</li><li><ul><li>Sub</li></ul></li><li>Third</li></ol>`)
	assert.Contains(t, result, "1. First")
	// The inner ul switches listKind to "ul", so nested items become bullet items
	assert.Contains(t, result, "- Sub")
	// After the inner ul ends, the outer ol context is restored — the third item
	// resumes numbering from where it left off.
	assert.Contains(t, result, "3. Third")
}

func TestHTMLToText_HeadingTags(t *testing.T) {
	result := HTMLToText(`<h1>Title</h1><h2>Subtitle</h2><p>Body</p>`)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Subtitle")
	assert.Contains(t, result, "Body")
}

func TestHTMLToText_Table(t *testing.T) {
	input := `<table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr></table>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Name")
	assert.Contains(t, result, "Age")
	assert.Contains(t, result, "Alice")
	assert.Contains(t, result, "30")
}

func TestHTMLToText_Blockquote(t *testing.T) {
	result := HTMLToText(`<blockquote>Quote text</blockquote>`)
	assert.Contains(t, result, "Quote text")
}

func TestHTMLToText_HTMLEntities(t *testing.T) {
	result := HTMLToText(`<p>5 &gt; 3 &amp; 3 &lt; 5</p>`)
	assert.Contains(t, result, "5 > 3 & 3 < 5")
}

func TestHTMLToText_MalformedHTML(t *testing.T) {
	result := HTMLToText(`<p>unclosed<div>nested<p>text`)
	assert.NotPanics(t, func() { HTMLToText(`<p>unclosed<div>nested<p>text`) })
	assert.Contains(t, result, "text")
}

func TestHTMLToText_EmptyDocument(t *testing.T) {
	result := HTMLToText(``)
	assert.Equal(t, "", result)
}

func TestHTMLToText_SelfClosingTags(t *testing.T) {
	result := HTMLToText(`<br/>Hello<hr/>World`)
	assert.Contains(t, result, "Hello")
	assert.Contains(t, result, "World")
}

func TestHTMLToText_ImageWithAltText(t *testing.T) {
	result := HTMLToText(`<img src="pic.jpg" alt="A photo">`)
	assert.Contains(t, result, "A photo")
}

func TestHTMLToText_ImageWithoutAlt(t *testing.T) {
	result := HTMLToText(`<img src="pic.jpg">`)
	assert.NotContains(t, result, "pic.jpg")
}

func TestHTMLToText_ImageGenericAltFiltered(t *testing.T) {
	// "Image" alt text is a common CMS placeholder, not useful information
	result := HTMLToText(`<img src="photo.jpg" alt="Image">`)
	assert.NotContains(t, result, "Image")
}

func TestHTMLToText_LabelTagsStripped(t *testing.T) {
	// Labels with a "for" attribute reference a stripped form control — strip them.
	result := HTMLToText(`<label for="name">Name</label><input type="text" id="name">`)
	assert.NotContains(t, result, "Name")
}

func TestHTMLToText_LabelWithoutForPreserved(t *testing.T) {
	// Labels without "for" may carry useful context on sites with non-semantic HTML.
	input := `<div><label>Phone</label>: 612-555-0100</div>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Phone")
	assert.Contains(t, result, "612-555-0100")
}

func TestHTMLToText_ARIAHiddenStripped(t *testing.T) {
	// aria-hidden elements (modals, off-canvas) should not appear
	input := `<div class="modal" aria-hidden="true">This is hidden modal content</div><p>Visible content</p>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Visible content")
	assert.NotContains(t, result, "hidden modal content")
}

func TestHTMLToText_SVGStripped(t *testing.T) {
	// SVG elements produce path noise, not readable text
	input := `<p>Before</p><svg viewBox="0 0 100 100"><path d="M10 10..."/></svg><p>After</p>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Before")
	assert.Contains(t, result, "After")
	assert.NotContains(t, result, "viewBox")
	assert.NotContains(t, result, "<path")
}

func TestHTMLToText_DeeplyNestedDivs(t *testing.T) {
	input := `<div><div><div><div>Hello</div></div></div></div>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Hello")
}

func TestHTMLToText_PreTagPreservation(t *testing.T) {
	input := `<p>before</p><pre>  indented  code  </pre><p>after</p>`
	result := HTMLToText(input)
	// Internal whitespace within <pre> should be preserved. Surrounding the
	// pre content with paragraphs prevents TrimSpace from eating the spaces.
	assert.Contains(t, result, "  indented  code")
}

func TestHTMLToText_BrTag(t *testing.T) {
	result := HTMLToText(`<p>Line1<br>Line2</p>`)
	assert.Contains(t, result, "Line1")
	assert.Contains(t, result, "Line2")
}

func TestHTMLToText_NoscriptStripping(t *testing.T) {
	result := HTMLToText(`<noscript>Enable JS</noscript><p>Content</p>`)
	assert.Contains(t, result, "Content")
	assert.NotContains(t, result, "Enable JS")
}

// TestHTMLToText_MixedContent tests a mix of heading, paragraph, links, and inline styling.
func TestHTMLToText_MixedContent(t *testing.T) {
	input := `<h1>Title</h1><p>Text with <a href="/link">a link</a> and <strong>bold</strong>.</p>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "a link (/link)")
	assert.Contains(t, result, "bold")
}

// ---------------------------------------------------------------------------
// Whitespace-only link suppression (walk-level fix)
// ---------------------------------------------------------------------------

func TestHTMLToText_LinkWithOnlyWhitespaceChildren(t *testing.T) {
	input := `<p>Before</p><a href="https://example.com">  </a><p>After</p>`
	result := HTMLToText(input)
	assert.NotContains(t, result, "https://example.com")
	assert.Contains(t, result, "Before")
	assert.Contains(t, result, "After")
}

func TestHTMLToText_LinkWithNestedWhitespaceOnly(t *testing.T) {
	input := `<a href="/home">  <span></span>  </a>`
	result := HTMLToText(input)
	assert.NotContains(t, result, "(/home)")
}

// ---------------------------------------------------------------------------
// Post-processing: orphan bullet joining
// ---------------------------------------------------------------------------

func TestPostProcess_OrphanBulletJoined(t *testing.T) {
	// Simulates what walk produces when a list item has whitespace before the link:
	// \n- \n    About (url)\n
	input := "- \n    About (https://example.com)"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "- About (https://example.com)", result)
}

func TestPostProcess_OrphanBulletWithBlankBetween(t *testing.T) {
	input := "- \n\n\n    About (https://example.com)"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "- About (https://example.com)", result)
}

func TestPostProcess_OrphanBulletNoNextLine(t *testing.T) {
	input := "- "
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "", result)
}

// ---------------------------------------------------------------------------
// Post-processing: whitespace collapse
// ---------------------------------------------------------------------------

func TestPostProcess_CollapsesBlankLinesWithTabs(t *testing.T) {
	// Simulates deeply nested empty divs that produce \n\t lines
	input := "Hello\n\t\n\n\t\n\t\nWorld"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "Hello\n\nWorld", result)
}

func TestPostProcess_CollapsesManyBlankLines(t *testing.T) {
	input := "A\n\n\n\n\n\n\n\n\n\nB"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "A\n\nB", result)
}

// ---------------------------------------------------------------------------
// Post-processing: useless line removal
// ---------------------------------------------------------------------------

func TestPostProcess_RemovesCAPTCHA(t *testing.T) {
	input := "Name\nCAPTCHA\nSubmit"
	result := postProcess(normalizeWhitespace(input))
	assert.NotContains(t, result, "CAPTCHA")
	assert.Contains(t, result, "Name")
	assert.Contains(t, result, "Submit")
}

func TestPostProcess_RemovesBareURL(t *testing.T) {
	input := "Before\n(https://example.com/path)\nAfter"
	result := postProcess(normalizeWhitespace(input))
	assert.NotContains(t, result, "(https://example.com/path)")
	assert.Contains(t, result, "Before")
	assert.Contains(t, result, "After")
}

func TestPostProcess_RemovesBareSlashURL(t *testing.T) {
	input := "Nav\n(/)\nFooter"
	result := postProcess(normalizeWhitespace(input))
	assert.NotContains(t, result, "(/)")
}

func TestPostProcess_RemovesBareTelURL(t *testing.T) {
	input := "(tel:+16122081638)"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "", result)
}

func TestPostProcess_PreservesParentheticalContent(t *testing.T) {
	// Real parenthetical text should not be removed
	input := "This is important (see note below)"
	result := postProcess(normalizeWhitespace(input))
	assert.Contains(t, result, "(see note below)")
}

func TestPostProcess_RemovesStandaloneImage(t *testing.T) {
	input := "Description\nImage\nMore text"
	result := postProcess(normalizeWhitespace(input))
	assert.NotContains(t, result, "Image")
	assert.Contains(t, result, "Description")
	assert.Contains(t, result, "More text")
}

// ---------------------------------------------------------------------------
// Post-processing: navigation deduplication
// ---------------------------------------------------------------------------

func TestPostProcess_DeduplicatesBulletLinks(t *testing.T) {
	input := "- About (https://example.com/about)\n- Contact (https://example.com/contact)\nBody content here\n- About (https://example.com/about)\n- Contact (https://example.com/contact)\n- Careers (https://example.com/careers)"
	result := postProcess(normalizeWhitespace(input))
	assert.Equal(t, "- About (https://example.com/about)\n- Contact (https://example.com/contact)\nBody content here\n- Careers (https://example.com/careers)", result)
}

func TestPostProcess_DedupNormalizesWhitespace(t *testing.T) {
	// Same link with different internal whitespace is still deduplicated.
	// The first occurrence is kept as-is; the duplicate is removed.
	input := "- About  (https://example.com/about)\nBody\n- About (https://example.com/about)"
	result := postProcess(normalizeWhitespace(input))
	// First occurrence kept verbatim, second is deduplicated
	lines := strings.Split(result, "\n")
	assert.Equal(t, 2, len(lines))
	assert.Contains(t, lines[0], "About")
	assert.Contains(t, lines[0], "https://example.com/about")
	assert.Equal(t, "Body", lines[1])
}

func TestPostProcess_DedupPreservesNumberedLists(t *testing.T) {
	// Numbered lists should NOT be deduplicated
	input := "1. First\n2. Second\nSome text\n1. First again\n2. Second again"
	result := postProcess(normalizeWhitespace(input))
	assert.Contains(t, result, "1. First")
	assert.Contains(t, result, "1. First again")
}

// ---------------------------------------------------------------------------
// Post-processing: pre tag whitespace preservation
// ---------------------------------------------------------------------------

func TestHTMLToText_PreTagWhitespacePreserved(t *testing.T) {
	input := `<p>before</p><pre>  indented  code  </pre><p>after</p>`
	result := HTMLToText(input)
	// Leading spaces inside <pre> must survive postProcess trimming
	assert.Contains(t, result, "  indented  code")
}

// ---------------------------------------------------------------------------
// Integration: realistic noisy HTML
// ---------------------------------------------------------------------------

func TestHTMLToText_RealisticNoisyHTML(t *testing.T) {
	// Simulates a real webpage with nested empty divs, repeated nav, forms, etc.
	// Note: HTML indentation uses newlines between <li> and <a> to produce
	// the actual multi-line pattern seen on real websites.
	input := `<html>
<head><title>Test Site</title><meta name="description" content="A test site"></head>
<body>
<div class="header">
<div class="nav-wrapper">
<nav>
<ul>
<li>
<a href="/about">About</a>
</li>
<li>
<a href="/menu">Menu</a>
</li>
</ul>
</nav>
</div>
</div>

<div class="content">
<h1>Welcome</h1>
<p>This is real content about our restaurant.</p>
<img src="photo.jpg">
<img src="logo.jpg" alt="Our logo">
</div>

<div class="footer">
<ul>
<li>
<a href="/about">About</a>
</li>
<li>
<a href="/menu">Menu</a>
</li>
<li>
<a href="/contact">Contact</a>
</li>
</ul>
<label>CAPTCHA</label>
</div>
</body></html>`

	result := HTMLToText(input)

	// Metadata at top
	assert.Contains(t, result, "Title: Test Site")
	assert.Contains(t, result, "Description: A test site")

	// Body content preserved
	assert.Contains(t, result, "Welcome")
	assert.Contains(t, result, "real content about our restaurant")
	assert.Contains(t, result, "Our logo")

	// Nav links appear only once (first occurrence kept, duplicates deduplicated)
	aboutCount := strings.Count(result, "- About (/about)")
	assert.Equal(t, 1, aboutCount, "About link should appear exactly once")

	menuCount := strings.Count(result, "- Menu (/menu)")
	assert.Equal(t, 1, menuCount, "Menu link should appear exactly once")

	// Footer-only link preserved
	assert.Contains(t, result, "- Contact (/contact)")

	// Noise removed
	assert.NotContains(t, result, "CAPTCHA")

	// Should not have excessive blank lines (deeply nested empty <div>s are collapsed)
	blankBreaks := strings.Count(result, "\n\n")
	assert.LessOrEqual(t, blankBreaks, 10,
		"expected reasonable blank-line breaks, got %d", blankBreaks)
}

func TestHTMLToText_EmptyDivsCollapsed(t *testing.T) {
	input := `<html><body><div><div><div><div><div><p>Hello</p></div></div></div></div></div></body></html>`
	result := HTMLToText(input)
	assert.Equal(t, "Hello", result)
}

// ---------------------------------------------------------------------------
// Head metadata extraction
// ---------------------------------------------------------------------------

func TestHTMLToText_HeadTitleExtracted(t *testing.T) {
	input := `<html><head><title>My Page</title></head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title: My Page")
	assert.Contains(t, result, "Body")
	// Metadata should appear before body content
	assert.True(t, strings.Index(result, "Title: My Page") < strings.Index(result, "Body"))
}

func TestHTMLToText_HeadMetaDescription(t *testing.T) {
	input := `<html><head><meta name="description" content="A great page"></head><body><p>Hello</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Description: A great page")
	assert.Contains(t, result, "Hello")
}

func TestHTMLToText_HeadCanonicalURL(t *testing.T) {
	input := `<html><head><link rel="canonical" href="https://example.com/page"></head><body><p>Hi</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "URL: https://example.com/page")
}

func TestHTMLToText_OgMetaTags(t *testing.T) {
	input := `<html><head>
		<meta property="og:title" content="OG Title" />
		<meta property="og:description" content="OG Description" />
	</head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title: OG Title")
	assert.Contains(t, result, "Description: OG Description")
}

func TestHTMLToText_DescriptionTakesPrecedenceOverOgDescription(t *testing.T) {
	input := `<html><head>
		<meta name="description" content="Normal description" />
		<meta property="og:description" content="OG description" />
	</head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Description: Normal description")
	assert.NotContains(t, result, "OG description")
}

func TestHTMLToText_OgTitleFallsBackWhenNoTitleTag(t *testing.T) {
	input := `<html><head>
		<meta property="og:title" content="OG Only Title" />
	</head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title: OG Only Title")
}

func TestHTMLToText_HeadScriptStrippedMetaStillExtracted(t *testing.T) {
	input := `<html><head>
		<script>var x = 1;</script>
		<title>T</title>
		<meta name="description" content="D" />
	</head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title: T")
	assert.Contains(t, result, "Description: D")
	assert.NotContains(t, result, "var x = 1")
}

func TestHTMLToText_NoHeadNoMetadata(t *testing.T) {
	input := `<p>Just a paragraph</p>`
	result := HTMLToText(input)
	assert.Equal(t, "Just a paragraph", result)
}

func TestHTMLToText_HeadTextNotDuplicated(t *testing.T) {
	// Regression: <head> content must not leak into the body output.
	// The title text should appear exactly once (in the metadata prefix).
	input := `<html><head><title>Unique Title XYZ</title></head><body><p>Body</p></body></html>`
	result := HTMLToText(input)
	count := strings.Count(result, "Unique Title XYZ")
	assert.Equal(t, 1, count, "Title text should appear exactly once in metadata, not leaked into body")
}

func TestHTMLToText_TemplateTagStripped(t *testing.T) {
	input := `<template><div id="modal">Hidden template content</div></template><p>Visible</p>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Visible")
	assert.NotContains(t, result, "Hidden template content")
}

func TestHTMLToText_NestedOLInsideOL(t *testing.T) {
	input := `<ol><li>Outer 1</li><li><ol><li>Inner A</li><li>Inner B</li></ol></li><li>Outer 3</li></ol>`
	result := HTMLToText(input)
	assert.Contains(t, result, "1. Outer 1")
	assert.Contains(t, result, "Inner A")
	assert.Contains(t, result, "Inner B")
	assert.Contains(t, result, "Outer 3")
}

func TestHTMLToText_FullRealWorldExample(t *testing.T) {
	// Based on the EaTo restaurant example from the issue
	input := `<html><head>
	<meta name='robots' content='index, follow' />
	<title>EaTo - Delicious Italian Fare</title>
	<meta name="description" content="Located in the East Town Minneapolis neighborhood, EaTo is a cheerful oasis." />
	<link rel="canonical" href="https://eatompls.com/" />
	<meta property="og:locale" content="en_US" />
	<meta property="og:type" content="website" />
	<meta property="og:title" content="EaTo - Delicious Italian Fare" />
	<meta property="og:description" content="Located in the East Town Minneapolis neighborhood, EaTo is a cheerful oasis." />
	<meta property="og:url" content="https://eatompls.com/" />
	<meta property="og:site_name" content="EaTo" />
	<script type="application/ld+json">{"@type":"WebPage"}</script>
	</head><body><h1>Welcome to EaTo</h1><p>Great food here.</p></body></html>`
	result := HTMLToText(input)
	// Should have metadata
	assert.Contains(t, result, "Title: EaTo - Delicious Italian Fare")
	assert.Contains(t, result, "Description: Located in the East Town Minneapolis neighborhood, EaTo is a cheerful oasis.")
	assert.Contains(t, result, "URL: https://eatompls.com/")
	// Should have body content
	assert.Contains(t, result, "Welcome to EaTo")
	assert.Contains(t, result, "Great food here")
	// Should NOT have useless stuff
	assert.NotContains(t, result, "index, follow")
	assert.NotContains(t, result, "og:locale")
	assert.NotContains(t, result, "og:site_name")
	// JSON-LD script content should not appear
	assert.NotContains(t, result, "WebPage")
}

// ---------------------------------------------------------------------------
// normalizeWhitespace
// ---------------------------------------------------------------------------

func TestNormalizeWhitespace_MultipleNewlines(t *testing.T) {
	input := "line1\n\n\n\nline2"
	result := normalizeWhitespace(input)
	assert.Equal(t, "line1\n\nline2", result)
}

func TestNormalizeWhitespace_CRLF(t *testing.T) {
	input := "line1\r\nline2"
	result := normalizeWhitespace(input)
	assert.Equal(t, "line1\nline2", result)
}

func TestNormalizeWhitespace_EmptyString(t *testing.T) {
	result := normalizeWhitespace("")
	assert.Equal(t, "", result)
}

func TestNormalizeWhitespace_AlreadyClean(t *testing.T) {
	input := "hello world"
	result := normalizeWhitespace(input)
	assert.Equal(t, "hello world", result)
}

func TestNormalizeWhitespace_LeadingTrailingWhitespace(t *testing.T) {
	input := "  hello  \n  "
	result := normalizeWhitespace(input)
	assert.Equal(t, "hello", result)
}

func TestNormalizeWhitespace_ExactlyTwoNewlinesPreserved(t *testing.T) {
	input := "a\n\nb"
	result := normalizeWhitespace(input)
	assert.Equal(t, "a\n\nb", result)
}

// ---------------------------------------------------------------------------
// itoa
// ---------------------------------------------------------------------------

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{999, "999"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, itoa(tc.input))
		})
	}
}
