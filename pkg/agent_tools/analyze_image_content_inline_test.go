//go:build !js

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngBytes is a minimal valid 1x1 PNG.
var pngBytes = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
}

// SP-140: a multimodal primary that calls analyze_image_content gets the
// pixels in the tool result — not a description of a description.
func TestAnalyzeImageContent_PrimaryVisionReceivesPixels(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot with spaces.png")
	if err := os.WriteFile(imgPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{
		PrimaryAcceptsImages: func() bool { return true },
	}, map[string]any{"image_path": imgPath})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("expected inline image in tool result, got %d", len(res.Images))
	}
	if !strings.Contains(res.Output, "image attached inline") {
		t.Errorf("expected inline-vision note, got %q", res.Output)
	}
	if strings.Contains(res.Output, "Analyze this image and provide") {
		t.Error("delegated analysis JSON should not be produced for a vision primary")
	}
}

// A non-vision primary keeps the legacy delegated-analysis behavior.
// Isolated from real providers: empty config dir + cleared key env vars so
// CreateVisionClient finds nothing and AnalyzeImage reports
// unavailability as status JSON (the legacy no-vision behavior).
func TestAnalyzeImageContent_NonVisionPrimaryDelegates(t *testing.T) {
	// Custom providers are HOME/XDG-global by design (SP-133), so isolate
	// both XDG_CONFIG_HOME and SPROUT_* to throwaway dirs — this machine
	// has real credentials and vision-capable custom providers on disk.
	tmpHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(tmpHome, "cfg"))
	t.Setenv("SPROUT_CONFIG", filepath.Join(tmpHome, "cfg", "config.json"))
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(tmpHome, "state"))
	for _, key := range []string{
		"DEEPINFRA_API_KEY", "OPENROUTER_API_KEY", "OPENAI_API_KEY",
		"ZAI_API_KEY", "ZAI_CODING_API_KEY", "MISTRAL_API_KEY",
		"DEEPSEEK_API_KEY", "CEREBRAS_API_KEY", "CHUTES_API_KEY",
		"MINIMAX_API_KEY", "OLLAMA_API_KEY",
	} {
		t.Setenv(key, "")
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{"image_path": imgPath})
	if err != nil {
		t.Fatalf("execute should not error (analysis JSON reports status): %v", err)
	}
	// Legacy path marker: the status JSON, not the inline note.
	if strings.Contains(res.Output, "image attached inline") {
		t.Error("non-vision primary must not get the inline-vision note")
	}
	if !strings.Contains(res.Output, `"success":false`) {
		t.Logf("no vision capability should produce the status JSON, got %.120q", res.Output)
	}
}

// Attachment failure (missing file) falls through to the analysis path
// rather than erroring.
func TestAnalyzeImageContent_PrimaryVisionFallbackOnBadPath(t *testing.T) {
	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{
		PrimaryAcceptsImages: func() bool { return true },
	}, map[string]any{"image_path": "/nonexistent/nope.png"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Images) != 0 {
		t.Error("missing file cannot produce pixels")
	}
	if !strings.Contains(res.Output, "no vision capability") && !strings.Contains(res.Output, "not_found") &&
		!strings.Contains(res.Output, "InputResolved") {
		t.Logf("note: output was %q", res.Output)
	}
}

// The URI form matters for downstream wire conversion: data-URI, PNG mime.
func TestAnalyzeImageContent_PrimaryVisionImageShape(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{
		PrimaryAcceptsImages: func() bool { return true },
	}, map[string]any{"image_path": imgPath})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	img := res.Images[0]
	if img.MIMEType != "image/png" {
		t.Errorf("mime = %q, want image/png", img.MIMEType)
	}
	if !strings.HasPrefix(img.URI, "data:image/png;base64,") {
		t.Errorf("expected data URI, got %.40q", img.URI)
	}
}
