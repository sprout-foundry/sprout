package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ============================================================================
// PDF Processing
//
// Two-stage pipeline:
//   1. Text extraction via Go-native ledongthuc/pdf — fast path for text-based
//      PDFs (no Python, no remote calls).
//   2. Page-rasterization OCR via the configured vision client — for scanned
//      / image-heavy PDFs that have no extractable text. Renders PNGs locally
//      via pypdfium2 and sends each page as an image.
// ============================================================================

const (
	maxPDFOCRImages = 4
	maxPDFOCRPages  = 8
)

// ProcessPDFWithVision processes a PDF file. Delegates to ProcessPDFForTextOnly.
func ProcessPDFWithVision(ctx context.Context, pdfPath string) (string, error) {
	return ProcessPDFForTextOnly(ctx, pdfPath)
}

// ProcessPDFForTextOnly extracts text from a PDF using Go-native extraction.
// Falls back to page-rasterization OCR if no text is found.
func ProcessPDFForTextOnly(ctx context.Context, pdfPath string) (string, error) {
	resolvedPath, cleanup, err := ResolvePDFInputPath(ctx, pdfPath)
	if err != nil {
		return "", fmt.Errorf("resolve PDF input path: %w", err)
	}
	defer cleanup()

	text, hasText, err := extractTextWithGoPDF(resolvedPath, 5000)
	if err == nil && hasText {
		return text, nil
	}

	client, err := CreateVisionClient()
	if err != nil {
		return "", fmt.Errorf("PDF has no extractable text and no vision client available for OCR: %w", err)
	}
	pythonExec, pythonErr := GetPDFPythonExecutable()
	if pythonErr != nil {
		return "", fmt.Errorf("PDF has no extractable text and Python is unavailable for page rasterization OCR: %w", pythonErr)
	}
	return processPDFForOCROnly(ctx, resolvedPath, pythonExec, client)
}

func processPDFForOCROnly(ctx context.Context, pdfPath, pythonExec string, client api.ClientInterface) (string, error) {
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("stat PDF file: %w", err)
	}
	maxSize := int64(50 * 1024 * 1024)
	if fileInfo.Size() > maxSize {
		return "", fmt.Errorf("PDF file too large (%d MB, maximum size is %d MB)", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}
	ocrText, ocrErr := processPDFWithOCR(ctx, pdfPath, pythonExec, client)
	if ocrErr == nil && len(strings.TrimSpace(ocrText)) > 0 {
		return ocrText, nil
	}
	return "", fmt.Errorf("PDF no extractable text: page OCR: %w", ocrErr)
}

func ResolvePDFInputPath(ctx context.Context, inputPath string) (string, func(), error) {
	if strings.HasPrefix(inputPath, "http://") || strings.HasPrefix(inputPath, "https://") {
		return downloadRemotePDFToTemp(ctx, inputPath)
	}
	return inputPath, func() {}, nil
}

func downloadRemotePDFToTemp(ctx context.Context, url string) (string, func(), error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var data []byte
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create PDF download request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download PDF: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			// Surface Retry-After for 429/503 and other 5xx.
			if resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode >= 500 {
				return &RetryableHTTPError{
					StatusCode: resp.StatusCode,
					Status:     resp.Status,
					Method:     req.Method,
					URL:        url,
					RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
				}
			}
			return fmt.Errorf("download PDF: status %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 60*1024*1024))
		if err != nil {
			return fmt.Errorf("read downloaded PDF bytes: %w", err)
		}
		return nil
	}, RetryOptions{OpName: "download_pdf"})
	if err != nil {
		return "", func() {}, err
	}
	if len(data) == 0 {
		return "", func() {}, fmt.Errorf("downloaded PDF is empty")
	}
	if !looksLikePDF(data) {
		return "", func() {}, fmt.Errorf("downloaded content is not a valid PDF")
	}
	tmp, err := os.CreateTemp("", "sprout_pdf_*.pdf")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp PDF file: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temp PDF file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("finalize temp PDF file: %w", err)
	}
	return tmp.Name(), cleanup, nil
}

// extractTextWithGoPDF extracts text using the Go-native ledongthuc/pdf library.
func extractTextWithGoPDF(pdfPath string, charLimit int) (string, bool, error) {
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", false, fmt.Errorf("stat PDF file: %w", err)
	}
	if fileInfo.Size() > 50*1024*1024 {
		return "", false, fmt.Errorf("PDF file too large (%d MB, max 50 MB)", fileInfo.Size()/1024/1024)
	}
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return "", false, fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()
	textReader, err := r.GetPlainText()
	if err != nil {
		return "", false, fmt.Errorf("extract PDF text: %w", err)
	}
	limited := io.LimitReader(textReader, int64(charLimit))
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", false, fmt.Errorf("read PDF text: %w", err)
	}
	text := string(data)
	hasText := len(strings.TrimSpace(text)) > 0
	return text, hasText, nil
}

func processPDFWithOCR(ctx context.Context, pdfPath, pythonExec string, client api.ClientInterface) (string, error) {
	pageImages, pageErr := extractPageImagesFromPDF(ctx, pdfPath, pythonExec)
	if pageErr == nil && len(pageImages) > 0 {
		if len(pageImages) > maxPDFOCRPages {
			pageImages = pageImages[:maxPDFOCRPages]
		}
		pageText, pageOCRErr := processOCRImages(ctx, pageImages, client, "Page")
		if pageOCRErr == nil && len(strings.TrimSpace(pageText)) > 0 {
			return pageText, nil
		}
	}
	images, err := extractImagesFromPDF(ctx, pdfPath, pythonExec)
	if err != nil {
		if pageErr != nil {
			return "", fmt.Errorf("page rasterization and image extraction: %w", err)
		}
		return "", fmt.Errorf("extract images from PDF: %w", err)
	}
	if len(images) == 0 {
		return "", fmt.Errorf("no images found in PDF (scanned PDF may be single raster image)")
	}
	if len(images) > maxPDFOCRImages {
		images = selectImagesForOCR(images, maxPDFOCRImages)
	}
	text, ocrErr := processOCRImages(ctx, images, client, "Image")
	if ocrErr != nil {
		if pageErr != nil {
			return "", fmt.Errorf("both OCR paths: page=%v, image=%w", pageErr, ocrErr)
		}
		return "", fmt.Errorf("OCR image processing: %w", ocrErr)
	}
	return text, nil
}

// processOCRImages is the entry point for OCR processing. It keeps the
// existing 4-arg signature for backward compatibility (used by tests in
// other packages) and delegates to the parallel implementation.
func processOCRImages(ctx context.Context, images [][]byte, client api.ClientInterface, sectionLabel string) (string, error) {
	return processOCRImagesParallel(ctx, images, client, sectionLabel, nil)
}

func extractPageImagesFromPDF(ctx context.Context, pdfPath, pythonExec string) ([][]byte, error) {
	cmd := exec.CommandContext(ctx, pythonExec, "-c", `
import sys, io, base64
try:
    import pypdfium2 as pdfium
except Exception as e:
    print(f'MISSING_PDFIUM: {e}', file=sys.stderr)
    sys.exit(3)
try:
    doc = pdfium.PdfDocument(sys.argv[1])
    images = []
    for i in range(len(doc)):
        page = doc[i]
        rendered = page.render(scale=2.0)
        pil_img = rendered.to_pil()
        out = io.BytesIO()
        pil_img.save(out, 'PNG')
        images.append(base64.b64encode(out.getvalue()).decode('ascii'))
    print('|'.join(images))
except Exception as e:
    print(f'Error: {e}', file=sys.stderr)
    sys.exit(1)
`, pdfPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pypdfium2 page render: %w", err)
	}
	return decodeBase64ImagePayload(output), nil
}

func extractImagesFromPDF(ctx context.Context, pdfPath, pythonExec string) ([][]byte, error) {
	cmd := exec.CommandContext(ctx, pythonExec, "-c", `
import sys, base64
try:
    from pypdf import PdfReader
    from PIL import Image
    import io
    reader = PdfReader(sys.argv[1])
    images = []
    for page_num, page in enumerate(reader.pages):
        if '/XObject' in page['/Resources']:
            xobjects = page['/Resources']['/XObject'].get_object()
            for obj in xobjects:
                if xobjects[obj]['/Subtype'] == '/Image':
                    try:
                        data = xobjects[obj].get_data()
                        filter_type = str(xobjects[obj].get('/Filter', ''))
                        if 'DCTDecode' in filter_type:
                            img = Image.open(io.BytesIO(data))
                        elif 'JPXDecode' in filter_type:
                            img = Image.open(io.BytesIO(data))
                        else:
                            width = xobjects[obj]['/Width']
                            height = xobjects[obj]['/Height']
                            color_mode = 'L'
                            if '/ColorSpace' in xobjects[obj]:
                                cs = str(xobjects[obj]['/ColorSpace'])
                                if 'RGB' in cs:
                                    color_mode = 'RGB'
                            img = Image.frombytes(color_mode, (width, height), data)
                        png_io = io.BytesIO()
                        img.save(png_io, 'PNG')
                        images.append(base64.b64encode(png_io.getvalue()).decode('ascii'))
                    except Exception as e:
                        print(f'Error extracting image: {{e}}', file=sys.stderr)
    print('|'.join(images))
except Exception as e:
    print(f'Error: {{e}}', file=sys.stderr)
    sys.exit(1)
`, pdfPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pypdf image extraction: %w", err)
	}
	return decodeBase64ImagePayload(output), nil
}

var dataURLPattern = regexp.MustCompile(`data:[^;\s]+;base64,[A-Za-z0-9+/=]+`)

func compactVisionError(err error) string {
	if err == nil {
		return "none"
	}
	msg := err.Error()
	msg = dataURLPattern.ReplaceAllStringFunc(msg, func(m string) string {
		mime := "application/octet-stream"
		if semi := strings.Index(m, ";"); semi > len("data:") {
			mime = m[len("data:"):semi]
		}
		return "data:" + mime + ";base64,[REDACTED]"
	})
	const maxChars = 800
	if len(msg) > maxChars {
		msg = msg[:maxChars] + "... (truncated)"
	}
	return msg
}

func looksLikePDF(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	return string(data[:5]) == "%PDF-"
}

func selectImagesForOCR(images [][]byte, maxImages int) [][]byte {
	if maxImages <= 0 || len(images) <= maxImages {
		return images
	}
	type imageCandidate struct {
		index int
		size  int
	}
	candidates := make([]imageCandidate, 0, len(images))
	for i, imageData := range images {
		candidates = append(candidates, imageCandidate{index: i, size: len(imageData)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].size == candidates[j].size {
			return candidates[i].index < candidates[j].index
		}
		return candidates[i].size > candidates[j].size
	})
	candidates = candidates[:maxImages]
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].index < candidates[j].index
	})
	selected := make([][]byte, 0, len(candidates))
	for _, candidate := range candidates {
		selected = append(selected, images[candidate.index])
	}
	return selected
}

func decodeBase64ImagePayload(output []byte) [][]byte {
	var images [][]byte
	if len(output) == 0 {
		return images
	}
	encoded := strings.TrimSpace(string(output))
	if encoded == "" {
		return images
	}
	for _, enc := range strings.Split(encoded, "|") {
		if enc == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(enc)
		if err == nil {
			images = append(images, data)
		}
	}
	return images
}

func SimplePDFInfo(pdfPath string) (map[string]interface{}, error) {
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("stat PDF file: %w", err)
	}
	maxSize := int64(20 * 1024 * 1024)
	if fileInfo.Size() > maxSize {
		return nil, fmt.Errorf("PDF file too large (%d MB, maximum size is %d MB)", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}
	f, r, err := pdf.Open(pdfPath)
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	info := make(map[string]interface{})
	info["page_count"] = r.NumPage()
	info["has_text"] = false
	for pageNum := 1; pageNum <= r.NumPage(); pageNum++ {
		p := r.Page(pageNum)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err == nil && strings.TrimSpace(text) != "" {
			info["has_text"] = true
			break
		}
	}
	return info, nil
}
