//go:build !js

package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// sniffMaxBytes bounds the magic-byte probe for unknown-content-type URLs.
// Image magic signatures live in the first ~32 bytes; AVIF's ftyp brand sits
// at offset 4-11; PDF needs "%PDF-" within the first 1KB. 64KB covers all of
// them with room for picky servers.
const sniffMaxBytes = 64 * 1024

// SniffURLContent resolves a URL whose Content-Type was missing or
// unrecognized (S3 signed URLs, CDN links, extension-less media endpoints).
// A size-capped ranged GET is magic-byte checked: images and PDFs route to
// the binary pipeline, everything else reports text so the caller falls
// back to the text handler (SP-140 Phase 3, URL gap fix).
//
// Returns (kind, effectiveURL, ok). ok=false means "treat as text" — either
// the sniff failed, the content is neither image nor PDF, or the size cap
// was hit before a signature appeared.
func SniffURLContent(ctx context.Context, url string) (ResponseKind, string, bool) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ResponseKindUnknown, url, false
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", sniffMaxBytes-1))

	resp, err := client.Do(req)
	if err != nil {
		return ResponseKindUnknown, url, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return ResponseKindUnknown, url, false
	}

	data, err := readUpTo(resp.Body, sniffMaxBytes)
	if err != nil || len(data) == 0 {
		return ResponseKindUnknown, url, false
	}

	if _, mimeType := console.DetectImageMagic(data); mimeType != "" {
		return ResponseKindImage, resp.Request.URL.String(), true
	}
	if isPDFSignature(data) {
		return ResponseKindPDF, resp.Request.URL.String(), true
	}

	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	switch {
	case contentType == "application/pdf":
		return ResponseKindPDF, resp.Request.URL.String(), true
	case strings.HasPrefix(contentType, "image/"):
		// Server insists it's an image but magic bytes disagree (rare,
		// exotic formats). Trust the header — the binary pipeline's own
		// magic-byte check will reject it gracefully if truly invalid.
		return ResponseKindImage, resp.Request.URL.String(), true
	}

	return ResponseKindUnknown, resp.Request.URL.String(), false
}

// readUpTo reads at most max bytes from r. Short reads and read errors are
// fine — sniffing works on whatever prefix arrived.
func readUpTo(r io.Reader, max int) ([]byte, error) {
	buf := make([]byte, max)
	n, err := io.ReadFull(r, buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// isPDFSignature checks for the "%PDF-" marker within the sniffed prefix.
func isPDFSignature(data []byte) bool {
	return bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-"))
}
