//go:build js

package tools

import (
	"context"
)

// OptimizeImageData — WASM stub. Returns data unchanged.
func OptimizeImageData(_ string, data []byte) ([]byte, string, error) {
	return data, "image/png", nil
}

// AnalyzeImage — WASM stub.
func AnalyzeImage(_ context.Context, _ string, _ string, _ string) (string, error) {
	return "vision analysis is not available in WASM mode", nil
}

// ProcessPDFForTextOnly — WASM stub.
func ProcessPDFForTextOnly(_ context.Context, _ string) (string, error) {
	return "", nil
}

// ResolvePDFInputPath — WASM stub.
func ResolvePDFInputPath(_ context.Context, inputPath string) (string, func(), error) {
	return inputPath, func() {}, nil
}

// ProcessImagesInText — WASM stub on VisionProcessor (type defined in vision_types.go).
func (vp *VisionProcessor) ProcessImagesInText(_ context.Context, text string) (string, []VisionAnalysis, error) {
	return text, nil, nil
}
