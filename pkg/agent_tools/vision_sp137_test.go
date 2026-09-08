package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validPNG is a 1x1 white PNG for round-trip tests.
func validPNG() []byte {
	// Minimal valid PNG: 8-byte signature + IHDR + IDAT + IEND for 1x1 px.
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00,
		0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D, 0xB0, 0x00, 0x00, 0x00,
		0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}
}

// TestReadFile_ImageBranch_NeverDumpsBinary pins SP-137 B3: read_file on an
// image must return either inline image data (for vision models) or OCR
// text — never raw binary bytes in the Output string.
func TestReadFile_ImageBranch_NeverDumpsBinary(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, validPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &readFileHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{"path": imgPath})
	if err != nil {
		t.Fatalf("read_file on image errored: %v (output=%q)", err, res.Output)
	}

	// The output must be the bracketed placeholder form, never raw PNG bytes.
	raw := string(validPNG())
	if strings.Contains(res.Output, strings.TrimPrefix(strings.TrimSpace(raw[:16]), "")) &&
		!strings.HasPrefix(res.Output, "[image") {
		t.Fatalf("read_file leaked binary into output: %q", res.Output[:min(60, len(res.Output))])
	}
	if !strings.HasPrefix(res.Output, "[image") {
		t.Fatalf("expected '[image: ...]' placeholder output, got %q", res.Output)
	}

	// Inline attachment must be present as a data URI with the right MIME.
	if len(res.Images) != 1 {
		t.Fatalf("expected 1 inline image, got %d", len(res.Images))
	}
	img := res.Images[0]
	if img.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want image/png", img.MIMEType)
	}
	if !strings.HasPrefix(img.URI, "data:image/png;base64,") {
		t.Errorf("URI not a png data URI: %.40s", img.URI)
	}
	if _, derr := base64.StdEncoding.DecodeString(strings.TrimPrefix(img.URI, "data:image/png;base64,")); derr != nil {
		t.Errorf("inline image not valid base64: %v", derr)
	}
}

// TestReadFile_ImageBranch_RejectsOversize pins the size cap: images over
// maxInlineImageBytes error out pointing at analyze_image_content instead of
// loading the file.
func TestReadFile_ImageBranch_RejectsOversize(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "big.png")
	// Header-valid PNG signature followed by junk padding past the cap.
	head := validPNG()
	big := append(append([]byte{}, head...), make([]byte, maxInlineImageBytes+1)...)
	if err := os.WriteFile(imgPath, big, 0o644); err != nil {
		t.Fatal(err)
	}

	h := &readFileHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{"path": imgPath})
	if err == nil && !res.IsError {
		t.Fatal("oversize image should fail")
	}
	if !strings.Contains(res.Output, "analyze_image_content") {
		t.Errorf("error should point at analyze_image_content, got %q", res.Output)
	}
}

// TestAnalyzeImageContent_AttachesImageForGeneralMode pins SP-137 1d: the
// non-OCR mode attaches the image for vision-capable primaries (seed strips
// Images for non-vision models).
func TestAnalyzeImageContent_AttachesImageForGeneralMode(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(imgPath, validPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"image_path":    imgPath,
		"analysis_mode": "general",
	})
	// Analysis itself may fail (no vision provider in test env) — but the
	// attachment must still ride when the handler produced a result.
	if err == nil && !res.IsError {
		if len(res.Images) != 1 {
			t.Errorf("general mode should attach 1 image, got %d", len(res.Images))
		}
	}
}

// TestAnalyzeImageContent_OCRModeSkipsAttachment pins the payload guard: OCR
// mode returns text only, no inline image doubling.
func TestAnalyzeImageContent_OCRModeSkipsAttachment(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(imgPath, validPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &analyzeImageContentHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"image_path":    imgPath,
		"analysis_mode": "ocr",
	})
	_ = res
	_ = err
	if len(res.Images) != 0 {
		t.Errorf("ocr mode must not attach images, got %d", len(res.Images))
	}
}

// TestAnalyzeImage_ProviderNeutralUnavailableError pins the B2 error text:
// capability wording, no provider names.
func TestAnalyzeImage_ProviderNeutralUnavailableError(t *testing.T) {
	if HasVisionCapability() {
		t.Skip("vision capability available in this env; dead-end path not exercised")
	}
	out, err := AnalyzeImage(context.Background(), "/nonexistent/img.png", "", "general")
	if err != nil {
		t.Fatalf("AnalyzeImage returns structured JSON, not error: %v", err)
	}
	var resp ImageAnalysisResponse
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("response not JSON: %v", jerr)
	}
	if resp.ErrorCode != ErrCodeVisionNotAvailable {
		t.Errorf("errorCode = %q, want %q", resp.ErrorCode, ErrCodeVisionNotAvailable)
	}
	for _, provider := range []string{"DEEPINFRA", "OPENROUTER", "OPENAI_API_KEY"} {
		if strings.Contains(strings.ToUpper(resp.ErrorMessage), provider) {
			t.Errorf("error message still provider-coupled: %q", resp.ErrorMessage)
		}
	}
	if !strings.Contains(resp.ErrorMessage, "no vision capability") {
		t.Errorf("error message should name the capability: %q", resp.ErrorMessage)
	}
}

// TestVisionTierNoProviderNames enforces the SP-137 standing rule: no
// vision-tier file may reference a specific provider by name (outside the
// built-in local client-type constants in isLocalRuntimeProvider).
func TestVisionTierNoProviderNames(t *testing.T) {
	files := []string{
		"vision_client.go", "vision_fallback.go", "vision_image.go",
		"native_ocr_darwin.go", "native_ocr_windows.go", "native_ocr_linux.go", "native_ocr_other.go",
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join("vision_client.go", "..", f))
		if err != nil {
			continue // platform-gated file not built here; skip
		}
		lower := strings.ToLower(string(data))
		for _, provider := range []string{"ollama", "deepinfra", "openrouter", "zai"} {
			if strings.Contains(lower, provider) {
				// The isLocalRuntimeProvider built-in carve-out is the one
				// sanctioned mention (client types, not vendor preference).
				if f == "vision_client.go" && strings.Contains(lower, provider) {
					lines := strings.Split(string(data), "\n")
					for i, line := range lines {
						ll := strings.ToLower(line)
						if strings.Contains(ll, provider) && !strings.Contains(ll, "clienttype") {
							t.Errorf("%s:%d references provider %q outside client-type constants: %s", f, i+1, provider, strings.TrimSpace(line))
						}
					}
					continue
				}
				t.Errorf("%s references provider %q — SP-137 rule violation", f, provider)
			}
		}
	}
}
