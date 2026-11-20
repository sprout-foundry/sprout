//go:build js

package tools

import (
	"context"
	"errors"
)

// OptimizeImageData — WASM stub. Returns data unchanged.
func OptimizeImageData(_ string, data []byte) ([]byte, string, error) {
	return data, "image/png", nil
}

// AnalyzeImage — WASM stub. Signature mirrors the !js definition.
func AnalyzeImage(_ context.Context, _ string, _ string, _ string, _ interface{}) (string, error) {
	return "The image is attached to this result; look at it directly to judge it.", nil
}

// ProcessPDFForTextOnly — WASM stub.
func ProcessPDFForTextOnly(_ context.Context, _ string, _ interface{}) (string, error) {
	return "", nil
}

// ProcessPDFForTextOnlyWithProvider — WASM stub.
func ProcessPDFForTextOnlyWithProvider(_ context.Context, _ string, _ interface{}) (string, error) {
	return "", nil
}

// ResolvePDFInputPath — WASM stub.
func ResolvePDFInputPath(_ context.Context, inputPath string) (string, func(), error) {
	return inputPath, func() {}, nil
}

// AnalyzeImage — WASM stub on VisionProcessor. The browser build has no
// vision pipeline, so a remote image cannot be described.
func (vp *VisionProcessor) AnalyzeImage(_ context.Context, _ string, _ ...string) (VisionAnalysis, error) {
	return VisionAnalysis{}, errors.New("vision analysis is not available in WASM mode")
}

// AnalyzeImageBytes — WASM stub on VisionProcessor.
func (vp *VisionProcessor) AnalyzeImageBytes(_ context.Context, _ string, _ []byte, _ string, _ ...string) (VisionAnalysis, error) {
	return VisionAnalysis{}, errors.New("vision analysis is not available in WASM mode")
}

// ProcessImagesInText — WASM stub on VisionProcessor (type defined in vision_types.go).
func (vp *VisionProcessor) ProcessImagesInText(_ context.Context, text string) (string, []VisionAnalysis, error) {
	return text, nil, nil
}
