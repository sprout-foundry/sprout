package webcontent

import (
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

func TestHTMLToText_MixedContent(t *testing.T) {
	input := `<h1>Title</h1><p>Text with <a href="/link">a link</a> and <strong>bold</strong>.</p>`
	result := HTMLToText(input)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "a link (/link)")
	assert.Contains(t, result, "bold")
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
