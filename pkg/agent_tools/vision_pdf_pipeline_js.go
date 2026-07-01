//go:build js

package tools

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// PDFPipelineResult is a WASM stub type — the real definition lives in
// vision_pdf_pipeline.go (build tag: !js).
type PDFPipelineResult struct {
	Text   string
	Images []api.ImageData
	Source string
}

// ProcessPDFForMultimodal is a WASM stub — the real implementation lives in
// vision_pdf_pipeline.go (build tag: !js) and depends on gofpdf/OCR libraries
// that are unavailable in the WASM environment.
func ProcessPDFForMultimodal(_ context.Context, _ string) (*PDFPipelineResult, error) {
	return nil, nil
}

// BinaryFetchResult is a WASM stub type — the real definition lives in
// binary_fetch.go (build tag: !js).
type BinaryFetchResult struct {
	Images       []api.ImageData
	Text         string
	Source       string
	EffectiveURL string
}

// FetchBinaryURL — WASM stub.
func FetchBinaryURL(_ context.Context, _ string, _ ResponseKind) (*BinaryFetchResult, error) {
	return nil, nil
}
