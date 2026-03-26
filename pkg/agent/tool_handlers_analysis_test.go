package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestResolveVisionToolInputPathFallsBackToLatestAttachedUserImage(t *testing.T) {
	t.Parallel()

	agent := &Agent{
		messages: []api.Message{
			{
				Role:    "user",
				Content: "[image: pasted.png]",
				Images: []api.ImageData{{
					Base64: base64.StdEncoding.EncodeToString(validPNGBytesForAnalysisTest()),
					Type:   "image/png",
				}},
			},
		},
	}

	resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(agent, "/tmp/paste_20260326_104639_068fa3.png")
	if !usedFallback {
		t.Fatal("expected fallback to attached image")
	}
	defer cleanup()

	if filepath.Ext(resolvedPath) != ".png" {
		t.Fatalf("expected .png temp file, got %s", resolvedPath)
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("expected materialized temp file to exist: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected materialized image to contain data")
	}
}

func TestResolveVisionToolInputPathKeepsExistingLocalFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.png")
	if err := os.WriteFile(existingPath, validPNGBytesForAnalysisTest(), 0o644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(&Agent{}, existingPath)
	defer cleanup()

	if usedFallback {
		t.Fatal("did not expect fallback for existing file")
	}
	if resolvedPath != existingPath {
		t.Fatalf("expected original path, got %s", resolvedPath)
	}
}

func TestResolveVisionToolInputPathWithoutAttachedImageReturnsOriginalPath(t *testing.T) {
	t.Parallel()

	original := "/tmp/nonexistent-image.png"
	resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(&Agent{}, original)
	defer cleanup()

	if usedFallback {
		t.Fatal("did not expect fallback without attached image")
	}
	if resolvedPath != original {
		t.Fatalf("expected original path, got %s", resolvedPath)
	}
}

func validPNGBytesForAnalysisTest() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}
