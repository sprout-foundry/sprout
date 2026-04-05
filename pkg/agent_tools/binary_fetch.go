package tools

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/console"
)

const binaryDownloadMaxSize = 60 * 1024 * 1024 // 60MB (must exceed pdfMaxSizeForProcessing)

// BinaryFetchResult holds the result of fetching binary content from a URL.
// Exactly one of Images or Text will be meaningfully populated.
type BinaryFetchResult struct {
	Images       []api.ImageData // Populated for image URLs (and scanned PDFs)
	Text         string          // Populated for text-based PDFs
	Source       string          // Description of how content was obtained
	EffectiveURL string          // Post-redirect URL (differs from input if redirected)
}

// FetchBinaryURL downloads binary content from a URL and processes it
// for multimodal consumption based on the detected content type.
func FetchBinaryURL(url string, kind ResponseKind) (*BinaryFetchResult, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d fetching %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, binaryDownloadMaxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Detect and report truncation for large responses
	if resp.ContentLength > 0 && resp.ContentLength > binaryDownloadMaxSize {
		return nil, fmt.Errorf("content too large (%d bytes, max %d bytes)",
			resp.ContentLength, binaryDownloadMaxSize)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty response body")
	}

	effectiveURL := resp.Request.URL.String()

	switch kind {
	case ResponseKindImage:
		return processImageBinary(effectiveURL, data)
	case ResponseKindPDF:
		return processPDFBinary(effectiveURL, data)
	default:
		return nil, fmt.Errorf("unsupported content type for binary fetch: %s", kind)
	}
}

// processImageBinary validates and optimizes an image downloaded from a URL.
func processImageBinary(sourceURL string, data []byte) (*BinaryFetchResult, error) {
	// Validate magic bytes
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return nil, fmt.Errorf("URL content is not a valid image (failed magic bytes check)")
	}

	// Optimize: resize if dimensions exceed 4096px, compress if above threshold.
	// OptimizeImageData is idempotent for already-small images (early return).
	optimizedData, optimizedMIME, optErr := OptimizeImageData(sourceURL, data)
	if optErr == nil && len(optimizedData) > 0 {
		data = optimizedData
		if optimizedMIME != "" {
			mimeType = optimizedMIME
		}
	}

	// Final size check after optimization
	if len(data) > visionMaxImageFileSizeBytes {
		return nil, fmt.Errorf("image too large after optimization (%d bytes, max %d bytes)",
			len(data), visionMaxImageFileSizeBytes)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return &BinaryFetchResult{
		Images:       []api.ImageData{{Base64: encoded, Type: mimeType}},
		Source:       "downloaded_image",
		EffectiveURL: sourceURL,
	}, nil
}

// processPDFBinary saves a PDF to a temp file and processes it for multimodal.
func processPDFBinary(effectiveURL string, data []byte) (*BinaryFetchResult, error) {
	// Validate it looks like a PDF
	if !looksLikePDF(data) {
		return nil, fmt.Errorf("downloaded content is not a valid PDF (failed PDF header check)")
	}

	// Check size
	if len(data) > pdfMaxSizeForProcessing {
		return nil, fmt.Errorf("PDF too large (%d bytes, max %d bytes)",
			len(data), pdfMaxSizeForProcessing)
	}

	// Write to a temp file (ProcessPDFForMultimodal takes a file path)
	tmpFile, err := os.CreateTemp("", "ledit_pdf_*.pdf")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for PDF: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() { os.Remove(tmpPath) }

	// Restrict permissions to owner-only
	if chmodErr := tmpFile.Chmod(0600); chmodErr != nil {
		tmpFile.Close()
		cleanup()
		return nil, fmt.Errorf("failed to set temp file permissions: %w", chmodErr)
	}

	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		tmpFile.Close()
		cleanup()
		return nil, fmt.Errorf("failed to write PDF to temp file: %w", writeErr)
	}
	tmpFile.Close()

	result, err := ProcessPDFForMultimodal(tmpPath)
	cleanup()

	if err != nil {
		return nil, fmt.Errorf("PDF multimodal processing failed: %w", err)
	}

	// Map PDFPipelineResult to BinaryFetchResult
	if len(result.Images) > 0 {
		return &BinaryFetchResult{
			Images:       result.Images,
			Source:       "rendered_pages",
			EffectiveURL: effectiveURL,
		}, nil
	}

	if strings.TrimSpace(result.Text) != "" {
		return &BinaryFetchResult{
			Text:         result.Text,
			Source:       "extracted_text",
			EffectiveURL: effectiveURL,
		}, nil
	}

	return nil, fmt.Errorf("PDF processing produced no output")
}
