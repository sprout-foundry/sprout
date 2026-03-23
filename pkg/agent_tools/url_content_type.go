package tools

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ResponseKind classifies what kind of content a URL serves.
type ResponseKind int

const (
	ResponseKindUnknown ResponseKind = iota // Unable to determine or unsupported
	ResponseKindText                        // HTML, JSON, XML, plain text, etc.
	ResponseKindImage                       // PNG, JPEG, GIF, WebP, BMP, AVIF
	ResponseKindPDF                         // application/pdf
)

// IsBinary returns true if the ResponseKind represents binary content
// that should go through the multimodal pipeline rather than text extraction.
func (k ResponseKind) IsBinary() bool {
	return k == ResponseKindImage || k == ResponseKindPDF
}

// String returns a human-readable name for debugging.
func (k ResponseKind) String() string {
	switch k {
	case ResponseKindText:
		return "text"
	case ResponseKindImage:
		return "image"
	case ResponseKindPDF:
		return "pdf"
	default:
		return "unknown"
	}
}

const maxRedirects = 10

// ProbeURLContentType sends a HEAD request to determine the kind of content
// a URL serves. Falls back to URL path extension if the HEAD request fails.
// Returns both the ResponseKind and the effective URL (after redirects).
func ProbeURLContentType(url string) (ResponseKind, string) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}

	resp, err := client.Head(url)
	if err != nil {
		// HEAD failed — fall back to path extension
		return classifyByPathExtension(url), url
	}
	defer resp.Body.Close()

	effectiveURL := resp.Request.URL.String()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		kind := ClassifyContentType(contentType, effectiveURL)
		if kind != ResponseKindUnknown {
			return kind, effectiveURL
		}
	}

	// Content-Type was empty or unrecognized — try path extension
	return classifyByPathExtension(effectiveURL), effectiveURL
}

// ClassifyContentType maps a Content-Type header value to a ResponseKind.
// Falls back to URL path extension when the header is ambiguous.
func ClassifyContentType(contentType string, urlPath string) ResponseKind {
	ct := strings.ToLower(strings.SplitN(contentType, ";", 2)[0])
	ct = strings.TrimSpace(ct)

	switch {
	case ct == "application/pdf":
		return ResponseKindPDF
	case strings.HasPrefix(ct, "image/svg"):
		return ResponseKindText // SVG is text-based XML
	case strings.HasPrefix(ct, "image/"):
		return ResponseKindImage
	}

	// Fall back to path extension as a hint
	return classifyByPathExtension(urlPath)
}

// classifyByPathExtension returns a ResponseKind based solely on the URL path extension.
// For HTTP(s) URLs, only the path component (before query/fragment) is examined.
func classifyByPathExtension(urlPath string) ResponseKind {
	// For URLs, only look at the path component (ignore query string and fragment)
	lookup := urlPath
	if strings.HasPrefix(strings.ToLower(urlPath), "http://") || strings.HasPrefix(strings.ToLower(urlPath), "https://") {
		if idx := strings.IndexAny(urlPath, "?#"); idx >= 0 {
			lookup = urlPath[:idx]
		}
	}
	ext := strings.ToLower(GetFileExtension(lookup))
	switch ext {
	case ".pdf":
		return ResponseKindPDF
	case ".svg":
		return ResponseKindText // SVG is text-based XML
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif":
		return ResponseKindImage
	default:
		return ResponseKindUnknown
	}
}
