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
		t.Fatalf("expected 2 parts (image + text), got %d", len(parts))
	}
	// image part first (Anthropic recommends images before text)
	imagePart := parts[0]
	if imagePart["type"] != "image_url" {
		t.Errorf("first part should be image_url, got %v", imagePart["type"])
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
	// text part second
	if parts[1]["type"] != "text" {
		t.Errorf("second part should be text, got %v", parts[1]["type"])
	}
}

func TestBuildMultiModalContent_ImageNoCacheControlWhenDisabled(t *testing.T) {
	setCacheImagesEnv(t, "false")
	p := &GenericProvider{}

	images := []api.ImageData{{Base64: "iVBORw0KGgo=", Type: "image/png"}}
	out := p.buildMultiModalContent("describe", images)

	parts := out.([]map[string]interface{})
	// images come first (parts[0] = image, parts[1] = text)
	imagePart := parts[0]
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

	// Expect: [image1, image2, text] — images first, then text
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts (2 images + text), got %d", len(parts))
	}
	// both images have cache_control
	for i := 0; i <= 1; i++ {
		if parts[i]["type"] != "image_url" {
			t.Errorf("part %d should be image_url, got %v", i, parts[i]["type"])
			continue
		}
		cc, ok := parts[i]["cache_control"].(map[string]string)
		if !ok {
			t.Errorf("image part %d missing cache_control", i)
			continue
		}
		if cc["type"] != "ephemeral" {
			t.Errorf("image %d: cache_control.type=%v, want ephemeral", i, cc["type"])
		}
	}
	// text part has no cache_control
	if parts[2]["type"] != "text" {
		t.Errorf("part 2 should be text, got %v", parts[2]["type"])
	}
	if _, hasCC := parts[2]["cache_control"]; hasCC {
		t.Errorf("text part should NOT have cache_control")
	}
}

func TestBuildMultiModalContent_CacheControlInsideImageURLBlock(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}
	out := p.buildMultiModalContent("x", []api.ImageData{{Base64: "YQ=="}})
	parts := out.([]map[string]interface{})
	// parts[0] = image, parts[1] = text (images first)
	imagePart := parts[0]
	imageURL := imagePart["image_url"].(map[string]interface{})

	// cache_control should be at the top level of the image part, NOT
	// inside the image_url sub-map. Some providers reject the latter form.
	if _, ok := imageURL["cache_control"]; ok {
		t.Error("cache_control should NOT be inside the image_url sub-block")
	}
	if _, ok := imagePart["cache_control"]; !ok {
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

// =============================================================================
// SP-103-B3 — Image-then-text ordering for multimodal messages
//
// Anthropic docs recommend placing all image content blocks BEFORE any text
// blocks in the user's message content array.
// (https://docs.anthropic.com/en/docs/build-with-claude/vision)
// =============================================================================

// TestBuildMultiModalContent_ImagesBeforeText verifies the core invariant:
// all image blocks appear before any text block, with relative order preserved.
func TestBuildMultiModalContent_ImagesBeforeText(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	images := []api.ImageData{
		{Base64: "aW1nQQ==", Type: "image/png"}, // imgA
		{Base64: "aW1nQg==", Type: "image/png"}, // imgB
	}
	out := p.buildMultiModalContent("hello world", images)

	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", out)
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts (2 images + 1 text), got %d", len(parts))
	}

	// parts[0] = first image (imgA)
	if parts[0]["type"] != "image_url" {
		t.Errorf("parts[0].type should be image_url, got %v", parts[0]["type"])
	}
	img0URL := parts[0]["image_url"].(map[string]interface{})["url"].(string)
	if !strings.Contains(img0URL, "aW1nQQ==") {
		t.Errorf("parts[0] should contain imgA, got %v", img0URL)
	}

	// parts[1] = second image (imgB)
	if parts[1]["type"] != "image_url" {
		t.Errorf("parts[1].type should be image_url, got %v", parts[1]["type"])
	}
	img1URL := parts[1]["image_url"].(map[string]interface{})["url"].(string)
	if !strings.Contains(img1URL, "aW1nQg==") {
		t.Errorf("parts[1] should contain imgB, got %v", img1URL)
	}

	// parts[2] = text
	if parts[2]["type"] != "text" {
		t.Errorf("parts[2].type should be text, got %v", parts[2]["type"])
	}
	if parts[2]["text"] != "hello world" {
		t.Errorf("parts[2].text should be 'hello world', got %q", parts[2]["text"])
	}
}

// TestBuildMultiModalContent_TextOnlyWhenNoImages verifies that with zero images,
// the helper returns a single-element text array (not a bare string).
func TestBuildMultiModalContent_TextOnlyWhenNoImages(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	out := p.buildMultiModalContent("just text", nil)

	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", out)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("parts[0].type should be text, got %v", parts[0]["type"])
	}
	if parts[0]["text"] != "just text" {
		t.Errorf("parts[0].text should be 'just text', got %q", parts[0]["text"])
	}
}

// TestBuildMultiModalContent_ImagesOnlyWhenNoText verifies that with images but
// empty/whitespace text, only image blocks are returned (no text block).
func TestBuildMultiModalContent_ImagesOnlyWhenNoText(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	images := []api.ImageData{
		{Base64: "aW1nQQ==", Type: "image/png"},
		{Base64: "aW1nQg==", Type: "image/jpeg"},
	}
	out := p.buildMultiModalContent("   ", images)

	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", out)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (2 images, no text), got %d", len(parts))
	}

	for i := 0; i < 2; i++ {
		if parts[i]["type"] != "image_url" {
			t.Errorf("parts[%d].type should be image_url, got %v", i, parts[i]["type"])
		}
	}
}

// TestBuildMultiModalContent_RelativeOrderPreserved verifies that the original
// image order is preserved in the output (imgA before imgB).
func TestBuildMultiModalContent_RelativeOrderPreserved(t *testing.T) {
	setCacheImagesEnv(t, "")
	p := &GenericProvider{}

	images := []api.ImageData{
		{Base64: "aW1nQQ==", Type: "image/png"}, // imgA
		{Base64: "aW1nQg==", Type: "image/png"}, // imgB
		{Base64: "aW1nQw==", Type: "image/png"}, // imgC
	}
	out := p.buildMultiModalContent("describe", images)

	parts, ok := out.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", out)
	}
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts (3 images + 1 text), got %d", len(parts))
	}

	// Verify images appear in original order: imgA, imgB, imgC
	expected := []string{"aW1nQQ==", "aW1nQg==", "aW1nQw=="}
	for i, exp := range expected {
		if parts[i]["type"] != "image_url" {
			t.Errorf("parts[%d].type should be image_url, got %v", i, parts[i]["type"])
			continue
		}
		url := parts[i]["image_url"].(map[string]interface{})["url"].(string)
		if !strings.Contains(url, exp) {
			t.Errorf("parts[%d] should contain %s, got %v", i, exp, url)
		}
	}

	// Text is last
	if parts[3]["type"] != "text" {
		t.Errorf("parts[3].type should be text, got %v", parts[3]["type"])
	}
}
