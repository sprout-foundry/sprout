//go:build !js

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

const (
	pdfMultimodalTextLimit  = 20000
	pdfMaxSizeForProcessing = 50 * 1024 * 1024
)

type PDFPipelineResult struct {
	Text   string
	Images []api.ImageData
	Source string
}

func ProcessPDFForMultimodal(ctx context.Context, pdfPath string) (*PDFPipelineResult, error) {
	f, openErr := os.Open(pdfPath)
	if openErr != nil {
		return nil, fmt.Errorf("open PDF: %w", openErr)
	}
	var header [5]byte
	n, hdrErr := f.Read(header[:])
	f.Close()
	if hdrErr != nil || n < 5 {
		return nil, fmt.Errorf("read PDF header: %w", hdrErr)
	}
	if !looksLikePDF(header[:]) {
		return nil, fmt.Errorf("not a valid PDF file")
	}
	stat, statErr := os.Stat(pdfPath)
	if statErr != nil {
		return nil, fmt.Errorf("stat PDF: %w", statErr)
	}
	if stat.Size() > pdfMaxSizeForProcessing {
		return nil, fmt.Errorf("PDF file too large (%d MB, max %d MB)", stat.Size()/1024/1024, pdfMaxSizeForProcessing/1024/1024)
	}

	text, hasText, goErr := extractTextWithGoPDF(pdfPath, pdfMultimodalTextLimit)
	if goErr == nil && hasText && len(strings.TrimSpace(text)) > 0 {
		return &PDFPipelineResult{Text: text, Source: "go_pdf"}, nil
	}

	pythonExec, err := GetPDFPythonExecutable()
	if err != nil {
		return nil, fmt.Errorf("PDF has no extractable text and Python is unavailable for page rasterization: %w", err)
	}

	pageImages, pageErr := extractPageImagesFromPDF(ctx, pdfPath, pythonExec)
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
		return nil, fmt.Errorf("PDF no extractable text and page rendering: %w", pageErr)
	}
	return nil, fmt.Errorf("PDF has no extractable text and failed page rendering")
}
