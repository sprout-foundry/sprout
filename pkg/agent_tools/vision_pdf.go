package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ledongthuc/pdf"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// ============================================================================
// PDF Processing
// ============================================================================

// ProcessPDFWithVision processes a PDF file using Ollama with glm-ocr model
func ProcessPDFWithVision(pdfPath string) (string, error) {
	pythonExec, err := GetPDFPythonExecutable()
	if err != nil {
		return "", fmt.Errorf("PDF precheck failed: %w", err)
	}

	// Load config to check PDF OCR settings
	configManager, err := configuration.NewManager()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	config := configManager.GetConfig()

	// Check if PDF OCR is enabled
	if !config.PDFOCREnabled {
		return "", fmt.Errorf("PDF OCR is not enabled. Please enable PDF OCR in config with provider 'ollama' and model 'glm-ocr'")
	}

	// Use the configured provider and model
	provider := config.PDFOCRProvider
	model := config.PDFOCRModel

	// Add :latest suffix if not present (Ollama convention)
	if model != "" && !strings.Contains(model, ":") {
		model = model + ":latest"
	}

	if provider == "" || model == "" {
		return "", fmt.Errorf("PDF OCR provider and model must be configured")
	}

	// Process PDF with the configured provider
	text, err := processPDFWithProvider(pdfPath, provider, model, pythonExec)
	if err != nil {
		return "", fmt.Errorf("PDF OCR failed: %w", err)
	}

	return text, nil
}

// processPDFWithProvider processes a PDF using the specified provider and model
// Works cross-platform without system dependencies (poppler, tesseract, etc.)
func processPDFWithProvider(pdfPath, provider, model, pythonExec string) (string, error) {
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

	// If no text found, try OCR by extracting images from PDF
	if !hasText || len(strings.TrimSpace(text)) == 0 {
		// Try OCR by extracting images from PDF
		ocrText, ocrErr := processPDFWithOCR(pdfPath, provider, model, pythonExec)
		if ocrErr == nil && len(strings.TrimSpace(ocrText)) > 0 {
			return ocrText, nil
		}
		// If image extraction OCR failed, try sending PDF directly
		if ocrErr != nil {
			ocrText2, ocrErr2 := processPDFWithVisionModel(pdfPath, provider, model)
			if ocrErr2 == nil && len(strings.TrimSpace(ocrText2)) > 0 {
				return ocrText2, nil
			}
			// Return original error if all fail
			return "", fmt.Errorf("PDF has no extractable text and OCR failed: %w", ocrErr)
		}
	}

	return text, nil
}

// extractTextWithPypdf extracts text from PDF using pypdf
func extractTextWithPypdf(pdfPath, pythonExec string) (string, bool, error) {
	cmd := exec.Command(pythonExec, "-c", fmt.Sprintf(`
import sys
try:
    from pypdf import PdfReader
    reader = PdfReader('%s')
    text = ''
    for page in reader.pages:
        page_text = page.extract_text()
        if page_text:
            text += page_text + '\\n'
    print(text[:5000])  # Limit output
    if text.strip():
        sys.exit(0)
    else:
        sys.exit(1)  # No text found
except Exception as e:
    print(f'Error: {e}', file=sys.stderr)
    sys.exit(2)
`, pdfPath))

	output, err := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	if err == nil && exitCode == 0 {
		return string(output), true, nil
	}

	return "", false, fmt.Errorf("pypdf extraction failed: %s", string(output))
}

// processPDFWithOCR extracts images from PDF and uses vision model for OCR
// Cross-platform solution using pypdf for image extraction (BSD licensed)
func processPDFWithOCR(pdfPath, provider, model, pythonExec string) (string, error) {
	// Extract images from PDF using pypdf (cross-platform, no external deps)
	images, err := extractImagesFromPDF(pdfPath, pythonExec)
	if err != nil {
		return "", fmt.Errorf("failed to extract images from PDF: %w", err)
	}

	if len(images) == 0 {
		return "", fmt.Errorf("no images found in PDF (scanned PDF may be single raster image)")
	}

	// Create client
	var client api.ClientInterface
	switch provider {
	case "ollama":
		client, err = CreateOllamaClient(model)
		if err != nil {
			return "", fmt.Errorf("failed to create Ollama client: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported PDF OCR provider: %s", provider)
	}

	// Process each extracted image
	var allText strings.Builder
	for i, imgData := range images {
		imgBase64 := base64.StdEncoding.EncodeToString(imgData)

		// Determine image type from first bytes
		imgType := "image/png"
		if len(imgData) >= 4 {
			if imgData[0] == 0xFF && imgData[1] == 0xD8 {
				imgType = "image/jpeg"
			} else if imgData[0] == 0x89 && string(imgData[1:4]) == "PNG" {
				imgType = "image/png"
			}
		}

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
		response, err := client.SendChatRequest(messages, nil, "")
		if err != nil {
			continue // Try next image
		}

		if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
			if allText.Len() > 0 {
				allText.WriteString("\n\n--- Image ")
				allText.WriteString(fmt.Sprintf("%d", i+1))
				allText.WriteString(" ---\n\n")
			}
			allText.WriteString(response.Choices[0].Message.Content)
		}
	}

	if allText.Len() == 0 {
		return "", fmt.Errorf("OCR failed for all extracted images")
	}

	return allText.String(), nil
}

// extractImagesFromPDF extracts all images from a PDF using pypdf and Pillow
// Returns properly formatted PNG images for OCR
func extractImagesFromPDF(pdfPath, pythonExec string) ([][]byte, error) {
	cmd := exec.Command(pythonExec, "-c", fmt.Sprintf(`
import sys
import base64
try:
    from pypdf import PdfReader
    from PIL import Image
    import io
    
    reader = PdfReader('%s')
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
`, pdfPath))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pypdf image extraction failed: %s", string(output))
	}

	// Parse base64-encoded images
	var images [][]byte
	if len(output) > 0 {
		encoded := strings.TrimSpace(string(output))
		if encoded != "" {
			for _, enc := range strings.Split(encoded, "|") {
				if enc != "" {
					data, err := base64.StdEncoding.DecodeString(enc)
					if err == nil {
						images = append(images, data)
					}
				}
			}
		}
	}

	return images, nil
}

// processPDFWithVisionModel sends PDF directly to glm-ocr model for OCR
// This is cross-platform and doesn't require poppler or tesseract
func processPDFWithVisionModel(pdfPath, provider, model string) (string, error) {
	// Read PDF file
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}

	// Convert to base64
	pdfBase64 := base64.StdEncoding.EncodeToString(data)

	// Create client
	var client api.ClientInterface
	switch provider {
	case "ollama":
		client, err = CreateOllamaClient(model)
		if err != nil {
			return "", fmt.Errorf("failed to create Ollama client: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported PDF OCR provider: %s", provider)
	}

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
	response, err := client.SendChatRequest(messages, nil, "")
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from OCR model")
	}

	return response.Choices[0].Message.Content, nil
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
