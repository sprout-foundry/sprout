package tools

import (
	"testing"
)

func TestResponseKind_IsBinary(t *testing.T) {
	tests := []struct {
		name     string
		kind     ResponseKind
		expected bool
	}{
		{"unknown", ResponseKindUnknown, false},
		{"text", ResponseKindText, false},
		{"image", ResponseKindImage, true},
		{"pdf", ResponseKindPDF, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kind.IsBinary(); got != tt.expected {
				t.Errorf("IsBinary() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResponseKind_String(t *testing.T) {
	tests := []struct {
		name     string
		kind     ResponseKind
		expected string
	}{
		{"unknown", ResponseKindUnknown, "unknown"},
		{"text", ResponseKindText, "text"},
		{"image", ResponseKindImage, "image"},
		{"pdf", ResponseKindPDF, "pdf"},
		{"out-of-range", ResponseKind(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestClassifyContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		urlPath     string
		expected    ResponseKind
	}{
		// PDF
		{"pdf", "application/pdf", "https://example.com/doc", ResponseKindPDF},
		{"pdf upper", "APPLICATION/PDF", "https://example.com/doc", ResponseKindPDF},
		{"pdf with charset", "application/pdf; charset=utf-8", "https://example.com/doc", ResponseKindPDF},

		// Image
		{"image/png", "image/png", "https://example.com/img.png", ResponseKindImage},
		{"image/jpeg", "image/jpeg", "https://example.com/img.jpg", ResponseKindImage},
		{"image/webp", "image/webp", "https://example.com/img.webp", ResponseKindImage},
		{"image/gif", "image/gif", "https://example.com/img.gif", ResponseKindImage},
		{"image with charset", "image/png; charset=utf-8", "https://example.com/img.png", ResponseKindImage},

		// SVG is text-based
		{"image/svg+xml", "image/svg+xml", "https://example.com/img.svg", ResponseKindText},
		{"image/svg", "image/svg", "https://example.com/img.svg", ResponseKindText},

		// text/html falls back to path extension
		{"text/html with .pdf path", "text/html", "https://example.com/doc.pdf", ResponseKindPDF},
		{"text/html with .png path", "text/html", "https://example.com/img.png", ResponseKindImage},
		{"text/html with .svg path", "text/html", "https://example.com/img.svg", ResponseKindText},
		{"text/html no known extension", "text/html", "https://example.com/page", ResponseKindUnknown},
		{"text/html with charset", "text/html; charset=utf-8", "https://example.com/page", ResponseKindUnknown},

		// Empty content-type falls back to path extension
		{"empty ct with .pdf path", "", "https://example.com/doc.pdf", ResponseKindPDF},
		{"empty ct with .png path", "", "https://example.com/img.png", ResponseKindImage},
		{"empty ct with .svg path", "", "https://example.com/img.svg", ResponseKindText},
		{"empty ct no extension", "", "https://example.com/page", ResponseKindUnknown},

		// Unrecognized content-type falls back to path extension
		{"application/octet-stream with .pdf path", "application/octet-stream", "https://example.com/doc.pdf", ResponseKindPDF},
		{"text/plain with .html path", "text/plain", "https://example.com/page.html", ResponseKindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyContentType(tt.contentType, tt.urlPath)
			if got != tt.expected {
				t.Errorf("ClassifyContentType(%q, %q) = %v, want %v",
					tt.contentType, tt.urlPath, got, tt.expected)
			}
		})
	}
}

func TestClassifyByPathExtension(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		expected ResponseKind
	}{
		// PDF
		{"pdf", "https://example.com/doc.pdf", ResponseKindPDF},
		{"pdf local", "/path/to/doc.pdf", ResponseKindPDF},
		{"pdf uppercase", "https://example.com/doc.PDF", ResponseKindPDF},

		// Images
		{"png", "https://example.com/img.png", ResponseKindImage},
		{"jpg", "https://example.com/img.jpg", ResponseKindImage},
		{"jpeg", "https://example.com/img.jpeg", ResponseKindImage},
		{"gif", "https://example.com/img.gif", ResponseKindImage},
		{"webp", "https://example.com/img.webp", ResponseKindImage},
		{"bmp", "https://example.com/img.bmp", ResponseKindImage},
		{"avif", "https://example.com/img.avif", ResponseKindImage},

		// SVG is text
		{"svg", "https://example.com/img.svg", ResponseKindText},

		// Unknown
		{"html", "https://example.com/page.html", ResponseKindUnknown},
		{"txt", "https://example.com/file.txt", ResponseKindUnknown},
		{"go", "https://example.com/main.go", ResponseKindUnknown},
		{"no extension", "https://example.com/page", ResponseKindUnknown},

		// URL with query string — only path component should be used
		{"pdf with query", "https://example.com/doc.pdf?v=1", ResponseKindPDF},
		{"png with query", "https://example.com/img.png#section", ResponseKindImage},
		{"png with query and fragment", "https://example.com/img.png?v=1#section", ResponseKindImage},
		{"no ext with query", "https://example.com/page?q=1.pdf", ResponseKindUnknown},

		// Local path edge cases
		{"local pdf", "/home/user/doc.pdf", ResponseKindPDF},
		{"local svg", "C:\\users\\img.svg", ResponseKindText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyByPathExtension(tt.urlPath)
			if got != tt.expected {
				t.Errorf("classifyByPathExtension(%q) = %v, want %v",
					tt.urlPath, got, tt.expected)
			}
		})
	}
}
