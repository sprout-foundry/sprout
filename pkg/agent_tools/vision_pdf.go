package tools

import (
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

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ============================================================================
// PDF Processing
// ============================================================================

const (
	maxPDFOCRImages = 4
	maxPDFOCRPages  = 8
)

// ProcessPDFWithVision processes a PDF file using Ollama with glm-ocr model
func ProcessPDFWithVision(pdfPath string) (string, error) {
	resolvedPath, cleanup, err := ResolvePDFInputPath(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve PDF input path: %w", err)
	}
	defer cleanup()

	pythonExec, err := GetPDFPythonExecutable()
	if err != nil {
		return "", fmt.Errorf("failed PDF precheck: %w", err)
	}

	client, err := CreateVisionClient()
	if err != nil {
		return "", fmt.Errorf("failed to create vision client for PDF OCR: %w", err)
	}

	text, err := processPDFWithProvider(resolvedPath, pythonExec, client)
	if err != nil {
		return "", fmt.Errorf("PDF OCR failed: %w", err)
	}

	return text, nil
}

// ProcessPDFForTextOnly processes a PDF to extract text content.
// First tries pypdf text extraction (no vision client required).
// If text extraction fails and a vision client is available, falls back to OCR.
func ProcessPDFForTextOnly(pdfPath string) (string, error) {
	resolvedPath, cleanup, err := ResolvePDFInputPath(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve PDF input path: %w", err)
	}
	defer cleanup()

	pythonExec, pypdfErr := GetPDFPythonExecutable()
	if pypdfErr == nil {
		text, hasText, err := extractTextWithPypdf(resolvedPath, pythonExec)
		if err == nil && hasText && len(strings.TrimSpace(text)) > 0 {
			return text, nil
		}
	}

	// pypdf failed or no text — use OCR-only pipeline to avoid redundant pypdf call
	client, err := CreateVisionClient()
	if err != nil {
		return "", fmt.Errorf("PDF has no extractable text and no vision client available for OCR: %w", err)
	}
	if pythonExec != "" {
		return processPDFForOCROnly(resolvedPath, pythonExec, client)
	}

	return "", fmt.Errorf("PDF has no extractable text and Python is unavailable for page rasterization OCR")
}

// processPDFForOCROnly processes a PDF using OCR steps only (skips pypdf text extraction).
func processPDFForOCROnly(pdfPath, pythonExec string, client api.ClientInterface) (string, error) {
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat PDF file: %w", err)
	}

	maxSize := int64(50 * 1024 * 1024)
	if fileInfo.Size() > maxSize {
		return "", fmt.Errorf("PDF file too large (%d MB, maximum size is %d MB)", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}

	directOCRText, directOCRErr := processPDFWithVisionModel(pdfPath, client)
	if directOCRErr == nil && len(strings.TrimSpace(directOCRText)) > 0 {
		return directOCRText, nil
	}

	ocrText, ocrErr := processPDFWithOCR(pdfPath, pythonExec, client)
	if ocrErr == nil && len(strings.TrimSpace(ocrText)) > 0 {
		return ocrText, nil
	}

	return "", fmt.Errorf("PDF has no extractable text. direct OCR error: %s; page OCR error: %s",
		compactVisionError(directOCRErr), compactVisionError(ocrErr))
}

// The caller must invoke the returned cleanup function when done with the resolved path.
func ResolvePDFInputPath(inputPath string) (string, func(), error) {
	if strings.HasPrefix(inputPath, "http://") || strings.HasPrefix(inputPath, "https://") {
		return downloadRemotePDFToTemp(inputPath)
	}
	return inputPath, func() {}, nil
}

func downloadRemotePDFToTemp(url string) (string, func(), error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to download PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("failed to download PDF: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 60*1024*1024))
	if err != nil {
		return "", func() {}, fmt.Errorf("failed reading downloaded PDF bytes: %w", err)
	}
	if len(data) == 0 {
		return "", func() {}, fmt.Errorf("downloaded PDF is empty")
	}
	if !looksLikePDF(data) {
		return "", func() {}, fmt.Errorf("downloaded content is not a valid PDF")
	}

	tmp, err := os.CreateTemp("", "ledit_pdf_*.pdf")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create temp PDF file: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(tmp.Name())
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("failed to write temp PDF file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("failed to finalize temp PDF file: %w", err)
	}

	return tmp.Name(), cleanup, nil
}

// processPDFWithProvider processes a PDF using the specified provider and model
// Works cross-platform without system dependencies (poppler, tesseract, etc.)
func processPDFWithProvider(pdfPath, pythonExec string, client api.ClientInterface) (string, error) {
	// Check file size
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat PDF file: %w", err)
	}

	maxSize := int64(50 * 1024 * 1024) // 50MB for PDF OCR
	if fileInfo.Size() > maxSize {
		return "", fmt.Errorf("PDF file too large (%d MB), maximum size is %d MB", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}

	// First try to extract text using pypdf (works for text-based PDFs, cross-platform)
	text, hasText, err := extractTextWithPypdf(pdfPath, pythonExec)
	if err == nil && hasText && len(strings.TrimSpace(text)) > 0 {
		return text, nil
	}

	// For scanned/image-heavy PDFs, first try direct PDF OCR in a single model request.
	directOCRText, directOCRErr := processPDFWithVisionModel(pdfPath, client)
	if directOCRErr == nil && len(strings.TrimSpace(directOCRText)) > 0 {
		return directOCRText, nil
	}

	// If no text found, try OCR by extracting images from PDF (bounded work).
	if !hasText || len(strings.TrimSpace(text)) == 0 {
		ocrText, ocrErr := processPDFWithOCR(pdfPath, pythonExec, client)
		if ocrErr == nil && len(strings.TrimSpace(ocrText)) > 0 {
			return ocrText, nil
		}
		return "", fmt.Errorf("PDF has no extractable text. direct OCR error: %s; image OCR error: %s",
			compactVisionError(directOCRErr), compactVisionError(ocrErr))
	}

	return text, nil
}

// extractTextWithPypdf extracts text from PDF using pypdf (5000 char output limit).
func extractTextWithPypdf(pdfPath, pythonExec string) (string, bool, error) {
	cmd := newPypdfTextExtractionCommand(pythonExec, pdfPath, 5000)
	return executePypdfTextExtraction(cmd)
}

// processPDFWithOCR extracts images from PDF and uses vision model for OCR
// Cross-platform solution using pypdf for image extraction (BSD licensed)
func processPDFWithOCR(pdfPath, pythonExec string, client api.ClientInterface) (string, error) {
	// Prefer page-level rasterization so OCR is one request per page.
	pageImages, pageErr := extractPageImagesFromPDF(pdfPath, pythonExec)
	if pageErr == nil && len(pageImages) > 0 {
		if len(pageImages) > maxPDFOCRPages {
			pageImages = pageImages[:maxPDFOCRPages]
		}
		pageText, pageOCRErr := processOCRImages(pageImages, client, "Page")
		if pageOCRErr == nil && len(strings.TrimSpace(pageText)) > 0 {
			return pageText, nil
		}
	}

	// Extract images from PDF using pypdf (cross-platform, no external deps)
	images, err := extractImagesFromPDF(pdfPath, pythonExec)
	if err != nil {
		if pageErr != nil {
			return "", fmt.Errorf("failed page rasterization and image extraction: %w (rasterization error: %v)", err, pageErr)
		}
		return "", fmt.Errorf("failed to extract images from PDF: %w", err)
	}

	if len(images) == 0 {
		return "", fmt.Errorf("no images found in PDF (scanned PDF may be single raster image)")
	}

	if len(images) > maxPDFOCRImages {
		images = selectImagesForOCR(images, maxPDFOCRImages)
	}

	text, ocrErr := processOCRImages(images, client, "Image")
	if ocrErr != nil {
		if pageErr != nil {
			return "", fmt.Errorf("both OCR paths failed: page=%v, image=%w", pageErr, ocrErr)
		}
		return "", ocrErr
	}

	return text, nil
}

func processOCRImages(images [][]byte, client api.ClientInterface, sectionLabel string) (string, error) {
	var allText strings.Builder
	failures := 0
	for i, imgData := range images {
		imagePathHint := fmt.Sprintf("pdf_%s_%d.png", strings.ToLower(sectionLabel), i+1)
		preparedData := imgData
		imgType := detectImageMimeType(imagePathHint)

		optimizedData, optimizedMimeType, optErr := OptimizeImageData(imagePathHint, preparedData)
		if optErr == nil && len(optimizedData) > 0 {
			preparedData = optimizedData
			if optimizedMimeType != "" {
				imgType = optimizedMimeType
			}
		}
		if len(preparedData) > visionMaxImageFileSizeBytes {
			failures++
			if failures >= 2 {
				break
			}
			continue
		}

		imgBase64 := base64.StdEncoding.EncodeToString(preparedData)

		// Create prompt for OCR
		prompt := GetOCRPrompt()

		// Create message with image
		messages := []api.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []api.ImageData{{Base64: imgBase64, Type: imgType}},
			},
		}

		// Send request
		response, err := client.SendVisionRequest(messages, nil, "")
		if err != nil {
			failures++
			if failures >= 2 {
				break
			}
			continue
		}

		if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
			if allText.Len() > 0 {
				allText.WriteString("\n\n--- ")
				allText.WriteString(sectionLabel)
				allText.WriteString(" ")
				allText.WriteString(fmt.Sprintf("%d", i+1))
				allText.WriteString(" ---\n\n")
			}
			allText.WriteString(response.Choices[0].Message.Content)
		}
	}

	if allText.Len() == 0 {
		return "", fmt.Errorf("OCR failed for all extracted %ss", strings.ToLower(sectionLabel))
	}

	return allText.String(), nil
}

// extractPageImagesFromPDF rasterizes pages into PNGs for page-level OCR.
func extractPageImagesFromPDF(pdfPath, pythonExec string) ([][]byte, error) {
	cmd := exec.Command(pythonExec, "-c", `
import sys
import io
import base64

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
        # ~144 DPI equivalent: good OCR quality with manageable size.
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
		return nil, fmt.Errorf("pypdfium2 page render failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return decodeBase64ImagePayload(output), nil
}

// extractImagesFromPDF extracts all images from a PDF using pypdf and Pillow
// Returns properly formatted PNG images for OCR
func extractImagesFromPDF(pdfPath, pythonExec string) ([][]byte, error) {
	cmd := exec.Command(pythonExec, "-c", `
import sys
import base64
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
                        
                        # Handle different filter types
                        if 'DCTDecode' in filter_type:
                            # JPEG encoded - decode directly with PIL
                            img = Image.open(io.BytesIO(data))
                        elif 'JPXDecode' in filter_type:
                            # JPEG2000 - try to handle
                            img = Image.open(io.BytesIO(data))
                        else:
                            # Raw/FlateDecode - need to get dimensions
                            width = xobjects[obj]['/Width']
                            height = xobjects[obj]['/Height']
                            color_mode = 'L'
                            if '/ColorSpace' in xobjects[obj]:
                                cs = str(xobjects[obj]['/ColorSpace'])
                                if 'RGB' in cs:
                                    color_mode = 'RGB'
                            img = Image.frombytes(color_mode, (width, height), data)
                        
                        # Convert to PNG
                        png_io = io.BytesIO()
                        img.save(png_io, 'PNG')
                        png_data = png_io.getvalue()
                        images.append(base64.b64encode(png_data).decode('ascii'))
                    except Exception as e:
                        print(f'Error extracting image: {{e}}', file=sys.stderr)
    print('|'.join(images))
except Exception as e:
    print(f'Error: {{e}}', file=sys.stderr)
    sys.exit(1)
`, pdfPath)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pypdf image extraction failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return decodeBase64ImagePayload(output), nil
}

// processPDFWithVisionModel sends PDF directly to glm-ocr model for OCR
// This is cross-platform and doesn't require poppler or tesseract
func processPDFWithVisionModel(pdfPath string, client api.ClientInterface) (string, error) {
	// Read PDF file
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}
	if !looksLikePDF(data) {
		return "", fmt.Errorf("input is not a valid PDF file (missing %%PDF header)")
	}

	// Convert to base64
	pdfBase64 := base64.StdEncoding.EncodeToString(data)

	// Create prompt for OCR
	prompt := GetPDFOCRPrompt()

	// Create message with PDF - glm-ocr supports PDF natively
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: pdfBase64, Type: "application/pdf"}},
		},
	}

	// Send request to Ollama
	response, err := client.SendVisionRequest(messages, nil, "")
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from OCR model")
	}

	return response.Choices[0].Message.Content, nil
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

// SimplePDFInfo returns basic info about PDF file
func SimplePDFInfo(pdfPath string) (map[string]interface{}, error) {
	// Check file size before processing (limit to 20MB for safety)
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat PDF file: %w", err)
	}

	maxSize := int64(20 * 1024 * 1024) // 20MB
	if fileInfo.Size() > maxSize {
		return nil, fmt.Errorf("PDF file too large (%d MB), maximum size is %d MB", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}

	f, r, err := pdf.Open(pdfPath)
	defer func() {
		_ = f.Close()
	}()
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}

	info := make(map[string]interface{})
	info["page_count"] = r.NumPage()
	info["has_text"] = false

	// Check if PDF has extractable text
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
