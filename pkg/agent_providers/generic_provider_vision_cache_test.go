package providers

import (
	"os"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// =============================================================================
// SP-103-B1 — cache_control: ephemeral on image blocks
//
// Covers:
//   - Default ON: image blocks carry cache_control: {type: "ephemeral"}
//   - Disabled via SPROUT_VISION_CACHE_IMAGES=false: no cache_control
//   - LEDIT_VISION_CACHE_IMAGES=false (legacy) honored as fallback
//   - cache_control sits inside the image_url block, not at top level
//   - Text-only content returns plain string (no parts)
//   - Mixed text + image: cache_control on image only, not on text
// =============================================================================

func setCacheImagesEnv(t *testing.T, value string) {
	t.Helper()
	oldSPROUT, _ := os.LookupEnv("SPROUT_VISION_CACHE_IMAGES")
	oldLEDIT, _ := os.LookupEnv("LEDIT_VISION_CACHE_IMAGES")
	if value == "" {
		os.Unsetenv("SPROUT_VISION_CACHE_IMAGES")
		os.Unsetenv("LEDIT_VISION_CACHE_IMAGES")
	} else {
		os.Setenv("SPROUT_VISION_CACHE_IMAGES", value)
	}
	t.Cleanup(func() {
		if oldSPROUT == "" {
			os.Unsetenv("SPROUT_VISION_CACHE_IMAGES")
		} else {
			os.Setenv("SPROUT_VISION_CACHE_IMAGES", oldSPROUT)
		}
		if oldLEDIT == "" {
			os.Unsetenv("LEDIT_VISION_CACHE_IMAGES")
		} else {
			os.Setenv("LEDIT_VISION_CACHE_IMAGES", oldLEDIT)
		}
	})
}

func TestVisionCacheImagesEnabled_Default(t *testing.T) {
	setCacheImagesEnv(t, "")
	if !visionCacheImagesEnabled() {
		t.Error("default (no env) should enable cache_control injection")
	}
}

func TestVisionCacheImagesEnabled_Disabled(t *testing.T) {
	for _, v := range []string{"false", "False", "FALSE", "0", "no", "off"} {
		setCacheImagesEnv(t, v)
		if visionCacheImagesEnabled() {
			t.Errorf("env=%q should disable cache_control injection", v)
		}
	}
}

func TestVisionCacheImagesEnabled_Enabled(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE", "1", "yes", "on"} {
		setCacheImagesEnv(t, v)
		if !visionCacheImagesEnabled() {
			t.Errorf("env=%q should enable cache_control injection", v)
		}
	}
}

func TestVisionCacheImagesEnabled_LegacyFallback(t *testing.T) {
	// When SPROUT_ is empty and LEDIT_ has a value, the helper should
	// read the LEDIT_ value.
	os.Setenv("SPROUT_VISION_CACHE_IMAGES", "")
	defer os.Unsetenv("SPROUT_VISION_CACHE_IMAGES")
	os.Setenv("LEDIT_VISION_CACHE_IMAGES", "false")
	defer os.Unsetenv("LEDIT_VISION_CACHE_IMAGES")

	if visionCacheImagesEnabled() {
		t.Error("expected LEDIT_VISION_CACHE_IMAGES=false to disable cache_control")
	}
}

// =============================================================================
// buildMultiModalContent integration
// =============================================================================

func TestBuildMultiModalContent_ImageHasCacheControl(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	images := []api.ImageData{{Base64: "iVBORw0KGgo=", Type: "image/png"}}
	out := p.buildMultiModalContent("describe", images)

	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{} for content-with-images, got %T", out)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d", len(parts))
	}
	// text part
	if parts[0]["type"] != "text" {
		t.Errorf("first part should be text, got %v", parts[0]["type"])
	}
	// image part — must have cache_control
	imagePart := parts[1]
	if imagePart["type"] != "image_url" {
		t.Errorf("second part should be image_url, got %v", imagePart["type"])
	}
	imageURL, ok := imagePart["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("image_url field should be map, got %T", imagePart["image_url"])
	}
	if !strings.HasPrefix(imageURL["url"].(string), "data:image/png;base64,") {
		t.Errorf("expected base64 data URL, got %v", imageURL["url"])
	}
	cc, ok := imagePart["cache_control"].(map[string]string)
	if !ok {
		t.Fatalf("expected cache_control to be map[string]string, got %T (%v)", imagePart["cache_control"], imagePart["cache_control"])
	}
	if cc["type"] != "ephemeral" {
		t.Errorf("expected cache_control.type=ephemeral, got %v", cc)
	}
}

func TestBuildMultiModalContent_ImageNoCacheControlWhenDisabled(t *testing.T) {
	setCacheImagesEnv(t, "false")
	p := &GenericProvider{}

	images := []api.ImageData{{Base64: "iVBORw0KGgo=", Type: "image/png"}}
	out := p.buildMultiModalContent("describe", images)

	parts := out.([]map[string]interface{})
	imagePart := parts[1]
	if _, present := imagePart["cache_control"]; present {
		t.Errorf("expected NO cache_control when disabled, got %v", imagePart["cache_control"])
	}
}

func TestBuildMultiModalContent_TextOnly(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}
	out := p.buildMultiModalContent("just text", nil)
	// Text-only (no images) still returns a single-part array
	// `[{"type": "text", "text": "just text"}]` — the function only
	// returns a bare string when BOTH text and parts-list are empty.
	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{} (single text part), got %T", out)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "just text" {
		t.Errorf("expected single text part, got %v", parts[0])
	}
}

func TestBuildMultiModalContent_TextAlone_WithEmptyImagesArray(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}
	out := p.buildMultiModalContent("just text", []api.ImageData{})
	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{} (single text part), got %T", out)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "just text" {
		t.Errorf("expected single text part, got %v", parts[0])
	}
}

func TestBuildMultiModalContent_EmptyTextEmptyImages(t *testing.T) {
	// Both empty: returns the empty text (string fallback).
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}
	out := p.buildMultiModalContent("", nil)
	s, ok := out.(string)
	if !ok || s != "" {
		t.Errorf("expected empty string fallback, got %v (%T)", out, out)
	}
}

func TestBuildMultiModalContent_CacheControlOnlyOnImages(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	images := []api.ImageData{
		{Base64: "aGVsbG8=", Type: "image/png"},
		{Base64: "d29ybGQ=", Type: "image/jpeg"},
	}
	out := p.buildMultiModalContent("mixed", images)
	parts := out.([]map[string]interface{})

	// Expect: [text, image1, image2]
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts (text + 2 images), got %d", len(parts))
	}
	// text part has no cache_control
	if _, hasCC := parts[0]["cache_control"]; hasCC {
		t.Errorf("text part should NOT have cache_control")
	}
	// both images have cache_control
	for i := 1; i <= 2; i++ {
		cc, ok := parts[i]["cache_control"].(map[string]string)
		if !ok {
			t.Errorf("image part %d missing cache_control", i)
			continue
		}
		if cc["type"] != "ephemeral" {
			t.Errorf("image %d: cache_control.type=%v, want ephemeral", i, cc["type"])
		}
	}
}

func TestBuildMultiModalContent_CacheControlInsideImageURLBlock(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}
	out := p.buildMultiModalContent("x", []api.ImageData{{Base64: "YQ=="}})
	parts := out.([]map[string]interface{})
	imageURL := parts[1]["image_url"].(map[string]interface{})

	// cache_control should be at the top level of the image part, NOT
	// inside the image_url sub-map. Some providers reject the latter form.
	if _, ok := imageURL["cache_control"]; ok {
		t.Error("cache_control should NOT be inside the image_url sub-block")
	}
	if _, ok := parts[1]["cache_control"]; !ok {
		t.Error("cache_control should be at the top level of the image part")
	}
}

func TestBuildMultiModalContent_SkipInvalidImage(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	// Both empty — no URL, no Base64. buildImageURL returns "". These
	// should be skipped, leaving only the text part.
	images := []api.ImageData{{URL: "", Base64: ""}, {URL: "", Base64: ""}}
	out := p.buildMultiModalContent("hi", images)
	parts := out.([]map[string]interface{})

	if len(parts) != 1 {
		t.Errorf("expected only text part (invalid images skipped), got %d: %v", len(parts), parts)
	}
}
