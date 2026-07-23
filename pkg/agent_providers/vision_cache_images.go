package providers

import (
	"os"
	"strings"
)

// visionCacheImagesEnabled reports whether vision image blocks should be
// tagged with `cache_control: {type: "ephemeral"}` for prompt caching.
//
// Default: true. Set VISION_CACHE_IMAGES=false to disable.
// Rationale: prompt caching for images reduces cost substantially for
// image-heavy multi-turn conversations (e.g. PDF-vision OCR where the
// user asks follow-up questions about the same document). Anthropic,
// OpenAI, and most providers that support prompt caching can consume
// this field.
//
// Note: we read both `SPROUT_VISION_CACHE_IMAGES` and `SPROUT_VISION_CACHE_IMAGES`
// directly (rather than via `pkg/configuration`) to avoid a circular
// import — `pkg/configuration/api_keys.go` already imports this package.
func visionCacheImagesEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("SPROUT_VISION_CACHE_IMAGES"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("SPROUT_VISION_CACHE_IMAGES"))
	}
	if raw == "" {
		return true // default ON
	}
	switch raw {
	case "false", "False", "FALSE", "0", "no", "No", "NO", "off", "Off", "OFF":
		return false
	}
	return true
}
