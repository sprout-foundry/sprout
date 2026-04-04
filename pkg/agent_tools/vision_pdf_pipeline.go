package tools

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

const (
	// pdfMultimodalTextLimit is the maximum characters for pypdf text extraction
	// in the multimodal path. Multimodal models can handle more context than OCR
	// output, so this is higher than the 5000 char limit used in the OCR pipeline.
	pdfMultimodalTextLimit  = 20000
	pdfMaxSizeForProcessing = 50 * 1024 * 1024 // 50MB
)

// PDFPipelineResult holds the output of PDF processing for multimodal consumption.
type PDFPipelineResult struct {
	Text   string          // Extracted text from pypdf (if available)
	Images []api.ImageData // Page images for multimodal models (if text extraction failed)
	Source string          // "pypdf" or "page_images"
}

// ProcessPDFForMultimodal processes a PDF file for multimodal consumption.
// Step 1: Try pypdf text extraction (works for text-based PDFs).
// Step 2: If no text found, render pages to optimized images for the model to see directly.
func ProcessPDFForMultimodal(pdfPath string) (*PDFPipelineResult, error) {
	pythonExec, err := GetPDFPythonExecutable()
	if err != nil {
		return nil, fmt.Errorf("PDF processing requires Python 3.10+: %w", err)
	}

	// Fast validation: check PDF header and file size without reading entire file.
	f, openErr := os.Open(pdfPath)
	if openErr != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", openErr)
	}
	defer f.Close()

	var header [5]byte
	if _, hdrErr := io.ReadFull(f, header[:]); hdrErr != nil {
		return nil, fmt.Errorf("failed to read PDF header: %w", hdrErr)
	}
	if !looksLikePDF(header[:]) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	stat, statErr := f.Stat()
	if statErr != nil {
		return nil, fmt.Errorf("failed to stat PDF: %w", statErr)
	}
	if stat.Size() > pdfMaxSizeForProcessing {
		return nil, fmt.Errorf("PDF file too large (%d MB, max %d MB)", stat.Size()/1024/1024, pdfMaxSizeForProcessing/1024/1024)
	}

	// Step 1: Try pypdf text extraction with higher limit for multimodal models
	text, hasText, pypdfErr := extractTextWithPypdfMultimodal(pdfPath, pythonExec)
	if pypdfErr == nil && hasText && len(strings.TrimSpace(text)) > 0 {
		return &PDFPipelineResult{Text: text, Source: "pypdf"}, nil
	}

	// Step 2: Render pages to images for multimodal model to see directly
	pageImages, pageErr := extractPageImagesFromPDF(pdfPath, pythonExec)
	if pageErr == nil && len(pageImages) > 0 {
		if len(pageImages) > maxPDFOCRPages {
			pageImages = pageImages[:maxPDFOCRPages]
		}

		var images []api.ImageData
		for i, imgData := range pageImages {
			optimized, mime, optErr := OptimizeImageData(fmt.Sprintf("page_%d.png", i+1), imgData)
			if optErr == nil && len(optimized) > 0 {
				imgData = optimized
			}
			if mime == "" {
				mime = "image/png"
			}

			// Skip unreasonably large page images
			if len(imgData) > visionMaxImageFileSizeBytes {
				continue
			}

			encoded := base64.StdEncoding.EncodeToString(imgData)
			images = append(images, api.ImageData{Base64: encoded, Type: mime})
		}

		if len(images) > 0 {
			return &PDFPipelineResult{Images: images, Source: "page_images"}, nil
		}
	}

	if pageErr != nil {
		return nil, fmt.Errorf("PDF has no extractable text and page rendering failed: %w", pageErr)
	}
	return nil, fmt.Errorf("PDF has no extractable text and page rendering failed")
}

// extractTextWithPypdfMultimodal extracts text from a PDF using pypdf with a higher
// character limit than extractTextWithPypdf. Multimodal models can handle much more
// context than OCR output, so we allow up to pdfMultimodalTextLimit characters.
func extractTextWithPypdfMultimodal(pdfPath, pythonExec string) (string, bool, error) {
	// This uses the same approach as extractTextWithPypdf but with a higher
	// output limit (20000 chars instead of 5000) since the multimodal model
	// can handle more context directly.
	cmd := newPypdfTextExtractionCommand(pythonExec, pdfPath, pdfMultimodalTextLimit)
	return executePypdfTextExtraction(cmd)
}

// newPypdfTextExtractionCommand creates a Python command to extract text from a PDF
// with a specified character limit on the output.
func newPypdfTextExtractionCommand(pythonExec, pdfPath string, charLimit int) *exec.Cmd {
	return exec.Command(pythonExec, "-c", fmt.Sprintf(`
import sys
try:
    from pypdf import PdfReader
    reader = PdfReader(sys.argv[1])
    text = ''
    for page in reader.pages:
        page_text = page.extract_text()
        if page_text:
            text += page_text + '\n'
    print(text[:%d])  # Limit output
    if text.strip():
        sys.exit(0)
    else:
        sys.exit(1)  # No text found
except Exception as e:
    print(f'Error: {e}', file=sys.stderr)
    sys.exit(2)
`, charLimit), pdfPath)
}

// executePypdfTextExtraction runs a pypdf text extraction command and interprets the result.
func executePypdfTextExtraction(cmd *exec.Cmd) (string, bool, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil {
		return string(output), true, nil
	}
	errMsg := strings.TrimSpace(stderr.String())
	if errMsg == "" {
		errMsg = strings.TrimSpace(string(output))
	}
	return "", false, fmt.Errorf("pypdf extraction failed: %s", errMsg)
}
