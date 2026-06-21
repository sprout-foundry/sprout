package tools

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"image/color"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// looksLikePDF
// ---------------------------------------------------------------------------

func TestLooksLikePDF_FromPipeline(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		want  bool
	}{
		{"valid PDF 1.0", []byte("%PDF-1.0\nsome content"), true},
		{"valid PDF 1.4", []byte("%PDF-1.4\nsome content"), true},
		{"valid PDF 1.7", []byte("%PDF-1.7\nsome content"), true},
		{"valid PDF 2.0", []byte("%PDF-2.0\nsome content"), true},
		{"just header bytes", []byte("%PDF-"), true}, // exactly 5 bytes
		{"too short 4 bytes", []byte("%PDF"), false},
		{"too short empty", []byte{}, false},
		{"not a PDF", []byte("<html>hello</html>"), false},
		{"wrong prefix", []byte("PDF-1.7\n"), false},
		{"case sensitive", []byte("%pdf-1.7\n"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := looksLikePDF(tt.data)
			if got != tt.want {
				t.Errorf("looksLikePDF(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// OptimizeImageData
// ---------------------------------------------------------------------------

func createSmallPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a solid color (red)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		t.Fatalf("failed to create PNG: %v", err)
	}
	return buf.Bytes()
}

func createSmallJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a solid color (blue)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	if err != nil {
		t.Fatalf("failed to create JPEG: %v", err)
	}
	return buf.Bytes()
}

func TestOptimizeImageData_SmallPNG(t *testing.T) {
	t.Parallel()
	data := createSmallPNG(t, 10, 10)
	result, mime, err := OptimizeImageData("test.png", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if mime != "image/jpeg" {
		t.Errorf("expected mime image/jpeg, got %s", mime)
	}
}

func TestOptimizeImageData_SmallJPEG(t *testing.T) {
	t.Parallel()
	data := createSmallJPEG(t, 10, 10)
	result, mime, err := OptimizeImageData("test.jpg", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if mime != "image/jpeg" {
		t.Errorf("expected mime image/jpeg, got %s", mime)
	}
}

func TestOptimizeImageData_InvalidData(t *testing.T) {
	t.Parallel()
	// Garbage bytes that can't be decoded as an image
	// NOTE: This silently falls back to returning the original data.
	// Callers should be aware that no error is returned for unoptimizable data.
	data := []byte{0xFF, 0xFE, 0xFD, 0x00}
	result, mime, err := OptimizeImageData("test.png", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to returning original data with detected mime
	if !bytes.Equal(result, data) {
		t.Errorf("expected original data returned, got different data")
	}
	if mime != "image/png" {
		t.Errorf("expected mime image/png from extension, got %s", mime)
	}
}

func TestOptimizeImageData_EmptyData(t *testing.T) {
	t.Parallel()
	result, mime, err := OptimizeImageData("test.png", []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty data can't be decoded, so it falls back to original
	if !bytes.Equal(result, []byte{}) {
		t.Errorf("expected empty data returned, got len=%d", len(result))
	}
	if mime != "image/png" {
		t.Errorf("expected mime image/png from extension, got %s", mime)
	}
}

func TestOptimizeImageData_UnknownExtension(t *testing.T) {
	t.Parallel()
	data := createSmallPNG(t, 10, 10)
	result, mime, err := OptimizeImageData("test.xyz", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	// After decode+re-encode, mime is image/jpeg
	if mime != "image/jpeg" {
		t.Errorf("expected mime image/jpeg after re-encode, got %s", mime)
	}
}

func TestOptimizeImageData_NoOpForSmallImages(t *testing.T) {
	t.Parallel()
	// Small images should be re-encoded to JPEG without size optimization loops
	data := createSmallPNG(t, 20, 20)
	result, mime, err := OptimizeImageData("small.png", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if mime != "image/jpeg" {
		t.Errorf("expected mime image/jpeg, got %s", mime)
	}
}

func TestOptimizeImageData_MaintainsImageIntegrity(t *testing.T) {
	t.Parallel()
	// Create a small test image and verify the result can still be decoded
	data := createSmallPNG(t, 50, 50)
	result, _, err := OptimizeImageData("test.png", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the output is a valid image
	decoded, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("optimized result is not a valid image: %v", err)
	}
	// Dimensions should be preserved for small images
	bounds := decoded.Bounds()
	if bounds.Dx() != 50 || bounds.Dy() != 50 {
		t.Errorf("expected 50x50, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestOptimizeImageData_FallbackMimeFromPath(t *testing.T) {
	t.Parallel()
	// When data can't be decoded, mime should come from the file path.
	// NOTE: This silent fallback behavior is intentional — the function does
	// not return an error for unoptimizable data, instead returning the
	// original data with a mime type inferred from the file extension.
	data := []byte{0x00, 0x01, 0x02}
	_, mime, _ := OptimizeImageData("photo.jpeg", data)
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg from .jpeg extension, got %s", mime)
	}

	_, mime, _ = OptimizeImageData("photo.gif", data)
	if mime != "image/gif" {
		t.Errorf("expected image/gif from .gif extension, got %s", mime)
	}

	_, mime, _ = OptimizeImageData("photo.webp", data)
	if mime != "image/webp" {
		t.Errorf("expected image/webp from .webp extension, got %s", mime)
	}

	// Unknown extension should fall back to png
	_, mime, _ = OptimizeImageData("photo.xyz", data)
	if mime != "image/png" {
		t.Errorf("expected image/png for unknown extension, got %s", mime)
	}
}

// ---------------------------------------------------------------------------
// newPypdfTextExtractionCommand
// ---------------------------------------------------------------------------

func TestNewPypdfTextExtractionCommand(t *testing.T) {
	t.Parallel()
	cmd := newPypdfTextExtractionCommand(context.Background(), "python3", "/path/to/doc.pdf", 5000)
	// NOTE: This test depends on the 'python3' command being resolvable via
	// exec.Command (PATH lookup). On systems where python3 is not installed,
	// cmd.Path may be non-empty (just the command name) but the command
	// would fail if actually executed. We only verify the command structure
	// here, not execution.

	if len(cmd.Path) == 0 {
		t.Fatal("expected non-empty command path")
	}

	// Check arguments contain expected content
	args := cmd.Args
	if len(args) < 2 {
		t.Fatal("expected at least 2 arguments")
	}
	if args[len(args)-1] != "/path/to/doc.pdf" {
		t.Errorf("expected last arg to be PDF path, got %s", args[len(args)-1])
	}

	// The second arg should be "-c"
	if len(args) >= 2 && args[1] != "-c" {
		t.Errorf("expected second arg to be '-c', got %s", args[1])
	}

	// The third arg should contain the character limit
	if len(args) >= 3 && !strings.Contains(args[2], "print(text[:5000])") {
		t.Errorf("expected Python code with limit 5000, got %s", args[2])
	}
}

func TestNewPypdfTextExtractionCommand_DifferentLimit(t *testing.T) {
	t.Parallel()
	cmd := newPypdfTextExtractionCommand(context.Background(), "python3", "/path/to/doc.pdf", 20000)

	args := cmd.Args
	if len(args) < 3 {
		t.Fatal("expected at least 3 arguments")
	}
	if !strings.Contains(args[2], "print(text[:20000])") {
		t.Errorf("expected Python code with limit 20000, got %s", args[2])
	}
}

// ---------------------------------------------------------------------------
// executePypdfTextExtraction
// ---------------------------------------------------------------------------

func TestExecutePypdfTextExtraction_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh, not available on Windows")
	}
	// Use shell printf to simulate a successful Python output
	cmd := exec.Command("sh", "-c", "printf 'Hello, world\\n'")
	text, hasText, err := executePypdfTextExtraction(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasText {
		t.Fatal("expected hasText = true")
	}
	if strings.TrimSpace(text) != "Hello, world" {
		t.Errorf("expected 'Hello, world', got %q", text)
	}
}

func TestExecutePypdfTextExtraction_Failure(t *testing.T) {
	t.Parallel()
	// Use a command that will fail (nonexistent binary)
	cmd := exec.Command("nonexistent_binary_xyz", "arg1")
	text, hasText, err := executePypdfTextExtraction(cmd)
	if err == nil {
		t.Fatal("expected error for failed command")
	}
	if hasText {
		t.Error("expected hasText = false")
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if !strings.Contains(err.Error(), "pypdf extraction") {
		t.Errorf("unexpected error message: %v", err)
	}
}
