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

// DelegateImageDescriptions sends images to the caller's provider's vision
// model with the structured-description prompt and returns attributed
// descriptions. This is the inline chat path's delegation rung: it invokes
// the SAME pipeline as the vision tools (VisionProcessor + retry + OCR
// fallback), never a second implementation.
//
// providerType names the provider the turn is running on. Delegation stays
// inside it — a turn on provider A is never described by provider B's model,
// which is how an unrelated (and possibly retired) vision_model pin used to
// surface in a session that never configured it. Pass "" to use client
// directly (client must then be non-nil).
//
// Per-image failure degrades to a placeholder description — delegation must
// not fail the caller's turn.
func DelegateImageDescriptions(ctx context.Context, client api.ClientInterface, providerType api.ClientType, imagePath string) (DelegatedImage, error) {
	if client == nil {
		var err error
		client, err = CreateVisionClientWithProvider(providerType)
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
