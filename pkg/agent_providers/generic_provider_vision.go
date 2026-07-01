package providers

import (
	"context"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// SupportsVision returns whether the provider supports vision
func (p *GenericProvider) SupportsVision() bool {
	if !p.config.Models.SupportsVision {
		return false
	}

	currentModel := strings.TrimSpace(p.model)
	if currentModel == "" {
		currentModel = strings.TrimSpace(p.config.Defaults.Model)
	}
	if currentModel == "" {
		return false
	}

	if modelInfoHasVisionTag(p.config.GetModelInfo(currentModel)) {
		return true
	}

	visionModel := strings.TrimSpace(p.config.Models.VisionModel)
	if visionModel != "" && strings.EqualFold(currentModel, visionModel) {
		return true
	}

	return false
}

// SupportsConversationalVision returns whether the active model is suitable
// for inline multimodal chat messages. Currently equivalent to
// SupportsVision() because all GenericProvider entries that opt into vision
// are chat-format models; OCR-only clients (like OllamaLocalClient) override
// this method to return false for OCR-only tags.
func (p *GenericProvider) SupportsConversationalVision() bool {
	return p.SupportsVision()
}

// GetVisionModel returns the vision model
func (p *GenericProvider) GetVisionModel() string {
	if p.config.Models.VisionModel != "" {
		return p.config.Models.VisionModel
	}
	return p.model // Fallback to current model
}

// SendVisionRequest sends a vision request (for providers that support it)
func (p *GenericProvider) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	if !p.SupportsVision() {
		return nil, fmt.Errorf("provider %s does not support vision", p.config.Name)
	}

	// Use vision model if specified
	visionModel := p.GetVisionModel()
	if visionModel != p.model {
		originalModel := p.model
		p.model = visionModel
		defer func() { p.model = originalModel }()
	}

	return p.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

// buildMultiModalContent creates a multi-part content array for messages with images.
//
// When VISION_CACHE_IMAGES is enabled (default true for models that
// support prompt caching, like Anthropic Claude 3.5 Sonnet/GPT-4o/etc.),
// each image URL block carries `cache_control: {type: "ephemeral"}`. This
// marks the image for prompt caching — the provider will cache the
// image tokens across consecutive requests with the same image, which
// dramatically reduces cost on multi-turn image-heavy conversations
// (e.g. vision OCR of multi-page PDFs where the user asks several
// follow-up questions about the images).
//
// The cache_control key is placed inside the image_url object so
// providers that don't use the new top-level array form (e.g. OpenAI
// image_url blocks) still get the right wire format.
func (p *GenericProvider) buildMultiModalContent(text string, images []api.ImageData) interface{} {
	parts := make([]map[string]interface{}, 0, len(images)+1)
	cacheImages := visionCacheImagesEnabled()

	// Anthropic recommends placing all image blocks BEFORE any text blocks
	// for best cost/quality. We preserve relative order within each type.
	// (https://docs.anthropic.com/en/docs/build-with-claude/vision)

	// Add image parts first
	for _, img := range images {
		imageURL := p.buildImageURL(img)
		if imageURL == "" {
			// Skip invalid images - caller should ensure valid image data
			continue
		}
		imageBlock := map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": imageURL,
			},
		}
		if cacheImages {
			imageBlock["cache_control"] = map[string]string{"type": "ephemeral"}
		}
		parts = append(parts, imageBlock)
	}

	// Add text part after images
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}

	if len(parts) == 0 {
		return text // Fall back to text if no valid parts
	}
	return parts
}

// buildImageURL constructs the image URL from either a URL or base64 data
func (p *GenericProvider) buildImageURL(img api.ImageData) string {
	imageURL := strings.TrimSpace(img.URL)
	if imageURL == "" && strings.TrimSpace(img.Base64) != "" {
		mimeType := strings.TrimSpace(img.Type)
		if mimeType == "" {
			mimeType = "image/png"
		}
		imageURL = "data:" + mimeType + ";base64," + img.Base64
	}
	return imageURL
}
