//go:build !js

package tools

import (
	"context"
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// DelegatedImage is a structured description of one image produced by a
// delegated vision model (SP-140 Phase 3). Provenance records which
// provider/model described it, so the conversation can attribute the
// quality of seeing it got.
type DelegatedImage struct {
	Path        string
	Description string
	Provider    string
	Model       string
}

// DelegateImageDescriptions sends images to the best registry-driven vision
// model with the structured-description prompt and returns attributed
// descriptions. This is the inline chat path's delegation rung: it invokes
// the SAME pipeline as analyze_image_content (CreateVisionClient walk +
// VisionProcessor + retry + OCR fallback), never a second implementation.
//
// Per-image failure degrades to a placeholder description — delegation must
// not fail the caller's turn.
func DelegateImageDescriptions(ctx context.Context, client api.ClientInterface, imagePath string) (DelegatedImage, error) {
	if client == nil {
		var err error
		client, err = CreateVisionClient()
		if err != nil {
			return DelegatedImage{}, fmt.Errorf("create delegation vision client: %w", err)
		}
	}

	processor := &VisionProcessor{visionClient: client}
	analysis, err := processor.AnalyzeImage(ctx, imagePath, GetStructuredDescriptionPrompt())
	if err != nil {
		return DelegatedImage{}, err
	}

	return DelegatedImage{
		Path:        imagePath,
		Description: analysis.Description,
		Provider:    client.GetProvider(),
		Model:       client.GetModel(),
	}, nil
}
