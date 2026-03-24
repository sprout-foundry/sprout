package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// readImageAsImageData
// ---------------------------------------------------------------------------

// pngMagic is the 8-byte PNG signature plus minimal IHDR-like padding so the
// file is large enough to be plausible.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE}

// jpegMagic is a minimal JPEG header (SOI + APP0 marker start).
var jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

// gifMagic is a minimal GIF header.
var gifMagic = []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}

func TestReadImageAsImageData(t *testing.T) {
	t.Run("valid PNG file returns correct MIME type and base64", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "test.png")
		if err := os.WriteFile(p, pngMagic, 0o644); err != nil {
			t.Fatal(err)
		}

		img, rawSize, err := readImageAsImageData(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if img.Type != "image/png" {
			t.Errorf("expected MIME type image/png, got %q", img.Type)
		}
		if img.Base64 == "" {
			t.Error("expected non-empty base64 data")
		}
		if img.URL != "" {
			t.Errorf("expected empty URL, got %q", img.URL)
		}
		if rawSize != len(pngMagic) {
			t.Errorf("expected raw size %d, got %d", len(pngMagic), rawSize)
		}

		// Verify the base64 decodes back to the original bytes.
		decoded, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			t.Fatalf("base64 decode failed: %v", err)
		}
		if string(decoded) != string(pngMagic) {
			t.Error(" decoded base64 does not match original file content")
		}
	})

	t.Run("valid JPEG file returns correct MIME type", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "photo.jpg")
		if err := os.WriteFile(p, jpegMagic, 0o644); err != nil {
			t.Fatal(err)
		}

		img, rawSize, err := readImageAsImageData(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if img.Type != "image/jpeg" {
			t.Errorf("expected MIME type image/jpeg, got %q", img.Type)
		}
		if img.Base64 == "" {
			t.Error("expected non-empty base64 data")
		}
		if rawSize != len(jpegMagic) {
			t.Errorf("expected raw size %d, got %d", len(jpegMagic), rawSize)
		}

		decoded, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			t.Fatalf("base64 decode failed: %v", err)
		}
		if string(decoded) != string(jpegMagic) {
			t.Error("decoded base64 does not match original file content")
		}
	})

	t.Run("valid GIF file returns correct MIME type", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "anim.gif")
		if err := os.WriteFile(p, gifMagic, 0o644); err != nil {
			t.Fatal(err)
		}

		img, rawSize, err := readImageAsImageData(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if img.Type != "image/gif" {
			t.Errorf("expected MIME type image/gif, got %q", img.Type)
		}
		if img.Base64 == "" {
			t.Error("expected non-empty base64 data")
		}
		if rawSize != len(gifMagic) {
			t.Errorf("expected raw size %d, got %d", len(gifMagic), rawSize)
		}
	})

	t.Run("non-image file returns unrecognised image format error", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "not-an-image.txt")
		if err := os.WriteFile(p, []byte("this is plain text, not an image"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, _, err := readImageAsImageData(p)
		if err == nil {
			t.Fatal("expected error for non-image file, got nil")
		}
		if !strings.Contains(err.Error(), "unrecognised image format") {
			t.Errorf("expected 'unrecognised image format' error, got %q", err.Error())
		}
	})

	t.Run("non-existent file returns failed to stat file error", func(t *testing.T) {
		_, _, err := readImageAsImageData("/tmp/__nonexistent_ledit_test_file_12345.png")
		if err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
		if !strings.Contains(err.Error(), "failed to stat file") {
			t.Errorf("expected 'failed to stat file' error, got %q", err.Error())
		}
	})

	t.Run("empty file returns unrecognised image format error", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "empty.png")
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}

		_, _, err := readImageAsImageData(p)
		if err == nil {
			t.Fatal("expected error for empty file, got nil")
		}
		if !strings.Contains(err.Error(), "unrecognised image format") {
			t.Errorf("expected 'unrecognised image format' error, got %q", err.Error())
		}
	})

	t.Run("file too short for any magic bytes returns unrecognised image format", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "short.bin")
		if err := os.WriteFile(p, []byte{0x89, 0x50}, 0o644); err != nil {
			t.Fatal(err)
		}

		_, _, err := readImageAsImageData(p)
		if err == nil {
			t.Fatal("expected error for short file, got nil")
		}
		if !strings.Contains(err.Error(), "unrecognised image format") {
			t.Errorf("expected 'unrecognised image format' error, got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// processImagesInQuery — integration tests
// ---------------------------------------------------------------------------

func TestProcessImagesInQuery_NilClient_ReturnsQueryUnchanged(t *testing.T) {
	a := &Agent{client: nil}
	query := "Pasted image saved to disk: ./img.png — describe it"

	images, cleaned, err := a.processImagesInQuery(query)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if images != nil {
		t.Errorf("expected nil images, got %v", images)
	}
	if cleaned != query {
		t.Errorf("expected query to be unchanged, got %q", cleaned)
	}
}

// visionSupportingClient is a minimal mock that reports vision support.
type visionSupportingClient struct {
	api.ClientInterface // embed for unused methods
	supportsVision      bool
}

func (v *visionSupportingClient) SupportsVision() bool { return v.supportsVision }

func (v *visionSupportingClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return nil, nil
}
func (v *visionSupportingClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, nil
}
func (v *visionSupportingClient) CheckConnection() error             { return nil }
func (v *visionSupportingClient) SetDebug(debug bool)                {}
func (v *visionSupportingClient) SetModel(model string) error        { return nil }
func (v *visionSupportingClient) GetModel() string                   { return "test-vision-model" }
func (v *visionSupportingClient) GetProvider() string                { return "test" }
func (v *visionSupportingClient) GetModelContextLimit() (int, error) { return 4096, nil }
func (v *visionSupportingClient) GetLastTPS() float64                { return 0 }
func (v *visionSupportingClient) GetAverageTPS() float64             { return 0 }
func (v *visionSupportingClient) GetTPSStats() map[string]float64    { return nil }
func (v *visionSupportingClient) ResetTPSStats()                     {}
func (v *visionSupportingClient) GetVisionModel() string             { return "test-vision-model" }
func (v *visionSupportingClient) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return nil, nil
}
func (v *visionSupportingClient) ListModels() ([]api.ModelInfo, error) {
	return nil, nil
}

func TestProcessImagesInQuery_VisionClient_WithValidImages(t *testing.T) {
	// Save cwd so we can restore it after the test.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origCwd)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create the .ledit/pasted-images directory (mimics console behavior).
	pasteDir := filepath.Join(dir, ".ledit", "pasted-images")
	if err := os.MkdirAll(pasteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid PNG file inside the pasted-images directory.
	imgPath := filepath.Join(pasteDir, "paste_test_abc123.png")
	if err := os.WriteFile(imgPath, pngMagic, 0o644); err != nil {
		t.Fatal(err)
	}

	query := "Pasted image saved to disk: ./.ledit/pasted-images/paste_test_abc123.png — describe this screenshot"

	a := &Agent{client: &visionSupportingClient{supportsVision: true}}

	images, cleaned, err := a.processImagesInQuery(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) == 0 {
		t.Fatal("expected at least one image, got none")
	}

	if images[0].Type != "image/png" {
		t.Errorf("expected MIME type image/png, got %q", images[0].Type)
	}
	if images[0].Base64 == "" {
		t.Error("expected non-empty base64 in returned image")
	}

	// The query should have the placeholder replaced with "[image: paste_test_abc123.png]".
	if !strings.Contains(cleaned, "[image: paste_test_abc123.png]") {
		t.Errorf("expected cleaned query to contain [image: paste_test_abc123.png], got %q", cleaned)
	}
	// The original placeholder text should be gone.
	if strings.Contains(cleaned, "Pasted image saved to disk:") {
		t.Errorf("cleaned query should not contain the placeholder text, got %q", cleaned)
	}
}

func TestProcessImagesInQuery_VisionClient_NoPlaceholders_ReturnsQueryUnchanged(t *testing.T) {
	query := "Describe a sunset in three sentences"
	a := &Agent{client: &visionSupportingClient{supportsVision: true}}

	images, cleaned, err := a.processImagesInQuery(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if images != nil {
		t.Errorf("expected nil images, got %v", images)
	}
	if cleaned != query {
		t.Errorf("expected query unchanged, got %q", cleaned)
	}
}

func TestProcessImagesInQuery_NonVisionClient_InjectsToolPrompt(t *testing.T) {
	query := "Pasted image saved to disk: ./.ledit/pasted-images/test_a.png\nPlease read this image."
	a := &Agent{client: &visionSupportingClient{supportsVision: false}}

	images, cleaned, err := a.processImagesInQuery(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected no multimodal images for non-vision client, got %d", len(images))
	}
	if !strings.Contains(cleaned, "OCR Trigger Policy (MANDATORY)") {
		t.Fatalf("expected OCR trigger policy in prompt, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "analyze_image_content") {
		t.Fatalf("expected analyze_image_content tool instruction, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "./.ledit/pasted-images/test_a.png") {
		t.Fatalf("expected pasted image path in prompt, got: %q", cleaned)
	}
}

func TestProcessImagesInQuery_VisionClient_InvalidImagePath_SkipsImage(t *testing.T) {
	// Use a non-existent file path so readImageAsImageData fails.
	query := "Pasted image saved to disk: /tmp/__nonexistent_ledit_test_noimage.png — what is this?"

	a := &Agent{client: &visionSupportingClient{supportsVision: true}}

	images, cleaned, err := a.processImagesInQuery(query)
	if err != nil {
		t.Fatalf("unexpected error (invalid path should be skipped silently): %v", err)
	}

	// The image should be skipped, so images should be nil.
	if len(images) != 0 {
		t.Errorf("expected 0 images (invalid file should be skipped), got %d", len(images))
	}

	// The placeholder should still be cleaned from the query.
	if !strings.Contains(cleaned, "[image:") {
		t.Errorf("expected placeholder to be replaced in query, got %q", cleaned)
	}
}

func TestProcessImagesInQuery_VisionClient_OutsideContainmentDir_SkipsImage(t *testing.T) {
	// Save cwd so we can restore it after the test.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origCwd)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create the .ledit/pasted-images directory (required for the containment
	// check), but do NOT place any image inside it.
	pasteDir := filepath.Join(dir, ".ledit", "pasted-images")
	if err := os.MkdirAll(pasteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid PNG file in the temp dir root — OUTSIDE .ledit/pasted-images.
	escapedPath := filepath.Join(dir, "sibling.png")
	if err := os.WriteFile(escapedPath, pngMagic, 0o644); err != nil {
		t.Fatal(err)
	}

	// Craft a query whose placeholder points to the valid-but-containment-busting file.
	query := "Pasted image saved to disk: ./sibling.png — describe this screenshot"

	a := &Agent{client: &visionSupportingClient{supportsVision: true}}

	images, cleaned, err := a.processImagesInQuery(query)
	if err != nil {
		t.Fatalf("unexpected error (outside-containment path should be skipped silently): %v", err)
	}

	// The image must be rejected because it is not under .ledit/pasted-images/.
	if len(images) != 0 {
		t.Errorf("expected 0 images (file outside containment dir should be skipped), got %d", len(images))
	}

	// The placeholder should still be cleaned from the query text.
	if !strings.Contains(cleaned, "[image: sibling.png]") {
		t.Errorf("expected placeholder replaced with [image: sibling.png], got %q", cleaned)
	}
	if strings.Contains(cleaned, "Pasted image saved to disk:") {
		t.Errorf("cleaned query should not contain the placeholder text, got %q", cleaned)
	}
}
