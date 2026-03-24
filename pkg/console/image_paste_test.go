package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectImageMagic_PNG(t *testing.T) {
	// PNG magic: 89 50 4E 47 0D 0A 1A 0A
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	ext, mime := DetectImageMagic(data)
	if ext != ".png" {
		t.Errorf("expected .png, got %s", ext)
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
}

func TestDetectImageMagic_JPEG(t *testing.T) {
	// JPEG magic: FF D8 FF
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	ext, mime := DetectImageMagic(data)
	if ext != ".jpg" {
		t.Errorf("expected .jpg, got %s", ext)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}
}

func TestDetectImageMagic_GIF(t *testing.T) {
	// GIF magic: 47 49 46 38 (GIF8)
	data := []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61} // "GIF89a"
	ext, mime := DetectImageMagic(data)
	if ext != ".gif" {
		t.Errorf("expected .gif, got %s", ext)
	}
	if mime != "image/gif" {
		t.Errorf("expected image/gif, got %s", mime)
	}
}

func TestDetectImageMagic_WebP(t *testing.T) {
	// WebP magic: RIFF....WEBP (52 49 46 46 at 0, 57 45 42 50 at 8)
	data := make([]byte, 16)
	copy(data[0:4], []byte{0x52, 0x49, 0x46, 0x46}) // RIFF
	// bytes 4-7 are file size (don't care)
	copy(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}) // WEBP
	// bytes 12-15 are VP8 / VP8L / VP8X
	copy(data[12:16], []byte("VP8 "))

	ext, mime := DetectImageMagic(data)
	if ext != ".webp" {
		t.Errorf("expected .webp, got %s", ext)
	}
	if mime != "image/webp" {
		t.Errorf("expected image/webp, got %s", mime)
	}
}

func TestDetectImageMagic_BMP(t *testing.T) {
	// BMP magic: 42 4D (BM) — must also have zero reserved fields at bytes 6-9
	data := []byte{0x42, 0x4D, 0x3E, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	ext, mime := DetectImageMagic(data)
	if ext != ".bmp" {
		t.Errorf("expected .bmp, got %s", ext)
	}
	if mime != "image/bmp" {
		t.Errorf("expected image/bmp, got %s", mime)
	}
}

func TestDetectImageMagic_BMP_FalsePositive(t *testing.T) {
	// "BM" with non-zero reserved fields should NOT match (reduces false positives)
	data := []byte{0x42, 0x4D, 0x3E, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}
	ext, mime := DetectImageMagic(data)
	if ext != "" {
		t.Errorf("expected empty ext for BMP with non-zero reserved fields, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime for BMP with non-zero reserved fields, got %s", mime)
	}

	// Simple "BM" too short for full check should NOT match
	data2 := []byte{0x42, 0x4D, 0x00}
	ext2, _ := DetectImageMagic(data2)
	if ext2 != "" {
		t.Errorf("expected empty ext for short BMP data, got %s", ext2)
	}
}

func TestDetectImageMagic_AVIF(t *testing.T) {
	// AVIF starts with an ftyp box: bytes 4-7 are "ftyp" with avif/avis/mif1 brand
	data := make([]byte, 12)
	copy(data[4:8], []byte("ftyp"))
	// bytes 0-3 are ftyp box size
	data[0] = 0x00
	data[1] = 0x00
	data[2] = 0x00
	data[3] = 0x20
	copy(data[8:12], []byte("avif"))

	ext, mime := DetectImageMagic(data)
	if ext != ".avif" {
		t.Errorf("expected .avif, got %s", ext)
	}
	if mime != "image/avif" {
		t.Errorf("expected image/avif, got %s", mime)
	}

	// "avis" brand should also match
	data2 := make([]byte, 12)
	copy(data2[4:8], []byte("ftyp"))
	data2[8] = 'a'; data2[9] = 'v'; data2[10] = 'i'; data2[11] = 's'
	ext2, _ := DetectImageMagic(data2)
	if ext2 != ".avif" {
		t.Errorf("expected .avif for avis brand, got %s", ext2)
	}

	// "mif1" brand should also match
	data3 := make([]byte, 12)
	copy(data3[4:8], []byte("ftyp"))
	data3[8] = 'm'; data3[9] = 'i'; data3[10] = 'f'; data3[11] = '1'
	ext3, _ := DetectImageMagic(data3)
	if ext3 != ".avif" {
		t.Errorf("expected .avif for mif1 brand, got %s", ext3)
	}
}

func TestDetectImageMagic_AVIF_FalsePositive(t *testing.T) {
	// MP4 files also have "ftyp" but with "isom"/"mp42" brand — should NOT match
	data := make([]byte, 12)
	copy(data[4:8], []byte("ftyp"))
	copy(data[8:12], []byte("isom"))

	ext, mime := DetectImageMagic(data)
	if ext != "" {
		t.Errorf("expected empty ext for MP4 ftyp, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime for MP4 ftyp, got %s", mime)
	}

	// "mp42" brand should NOT match
	data2 := make([]byte, 12)
	copy(data2[4:8], []byte("ftyp"))
	copy(data2[8:12], []byte("mp42"))
	ext2, _ := DetectImageMagic(data2)
	if ext2 != "" {
		t.Errorf("expected empty ext for mp42 ftyp, got %s", ext2)
	}
}

func TestDetectImageMagic_NotAnImage(t *testing.T) {
	data := []byte("Hello, this is plain text data that is not an image.")
	ext, mime := DetectImageMagic(data)
	if ext != "" {
		t.Errorf("expected empty ext, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime, got %s", mime)
	}

	// All zeros
	data2 := make([]byte, 100)
	ext2, mime2 := DetectImageMagic(data2)
	if ext2 != "" {
		t.Errorf("expected empty ext for zeros, got %s", ext2)
	}
	if mime2 != "" {
		t.Errorf("expected empty mime for zeros, got %s", mime2)
	}
}

func TestDetectImageMagic_TooShort(t *testing.T) {
	ext, mime := DetectImageMagic([]byte{0x89, 0x50})
	if ext != "" {
		t.Errorf("expected empty ext for too-short data, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime for too-short data, got %s", mime)
	}

	ext, mime = DetectImageMagic([]byte{})
	if ext != "" {
		t.Errorf("expected empty ext for empty data, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime for empty data, got %s", mime)
	}
}

func TestDetectImageMagic_WebP_TooShort(t *testing.T) {
	// WebP needs 12 bytes; give only 8
	data := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00}
	ext, mime := DetectImageMagic(data)
	if ext != "" {
		t.Errorf("expected empty ext for too-short WebP, got %s", ext)
	}
	if mime != "" {
		t.Errorf("expected empty mime for too-short WebP, got %s", mime)
	}
}

func TestSavePastedImage(t *testing.T) {
	// Create a temporary directory and change CWD to it
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Build a minimal PNG (header + IHDR + IEND)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82, // IEND chunk
	}

	relPath, err := SavePastedImage(pngData)
	if err != nil {
		t.Fatalf("SavePastedImage failed: %v", err)
	}

	// Check the path format
	if !strings.HasPrefix(relPath, "./.ledit/pasted-images/paste_") {
		t.Errorf("unexpected path format: %s", relPath)
	}
	if !strings.HasSuffix(relPath, ".png") {
		t.Errorf("expected .png extension, got: %s", relPath)
	}

	// Verify the file exists on disk
	fullPath := filepath.Join(tmpDir, PastedImageDirName, filepath.Base(relPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("saved file does not exist: %v", err)
	}
	if info.Size() != int64(len(pngData)) {
		t.Errorf("expected file size %d, got %d", len(pngData), info.Size())
	}

	// Verify directory was created
	dirInfo, err := os.Stat(filepath.Join(tmpDir, PastedImageDirName))
	if err != nil {
		t.Fatalf("image directory does not exist: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Error("expected image directory to be a directory")
	}
}

func TestSavePastedImage_Oversized(t *testing.T) {
	// Build fake PNG data that exceeds the limit
	data := make([]byte, MaxPastedImageSize+1)
	copy(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	_, err := SavePastedImage(data)
	if err == nil {
		t.Fatal("expected error for oversized image, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("expected size error message, got: %v", err)
	}
}

func TestSavePastedImage_UnknownFormat(t *testing.T) {
	data := []byte("this is not an image at all")

	_, err := SavePastedImage(data)
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine image format") {
		t.Errorf("expected format error message, got: %v", err)
	}
}

func TestSavePastedImage_JPEG(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Minimal JPEG header
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	relPath, err := SavePastedImage(jpegData)
	if err != nil {
		t.Fatalf("SavePastedImage failed: %v", err)
	}
	if !strings.HasSuffix(relPath, ".jpg") {
		t.Errorf("expected .jpg extension, got: %s", relPath)
	}

	// Verify the file exists
	fullPath := filepath.Join(tmpDir, PastedImageDirName, filepath.Base(relPath))
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("saved file does not exist: %v", err)
	}
}
