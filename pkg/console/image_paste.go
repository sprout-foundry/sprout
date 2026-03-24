package console

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MaxPastedImageSize is the maximum size of a pasted image before rejection (10 MB).
const MaxPastedImageSize = 10 * 1024 * 1024

// PastedImageDirName is the subdirectory (relative to CWD) where pasted images are saved.
const PastedImageDirName = ".ledit/pasted-images"

// DetectImageMagic checks if data starts with a known image format signature.
// Returns the file extension (e.g., ".png") and MIME type (e.g., "image/png"),
// or empty strings if no known image format is detected.
func DetectImageMagic(data []byte) (ext string, mimeType string) {
	if len(data) < 3 {
		return "", ""
	}

	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return ".png", "image/png"
	}

	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return ".jpg", "image/jpeg"
	}

	// GIF: 47 49 46 38 ("GIF8")
	if len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return ".gif", "image/gif"
	}

	// WebP: RIFF....WEBP  (52 49 46 46 at 0, 57 45 42 50 at 8)
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return ".webp", "image/webp"
	}

	// BMP: 42 4D ("BM") — verify reserved fields are zero to reduce false positives
	if len(data) >= 10 &&
		data[0] == 0x42 && data[1] == 0x4D &&
		data[6] == 0x00 && data[7] == 0x00 &&
		data[8] == 0x00 && data[9] == 0x00 {
		return ".bmp", "image/bmp"
	}

	// AVIF: check for ftyp box at bytes 4-7 with avif/avis/mif1 brand
	if len(data) >= 12 &&
		data[4] == 0x66 && data[5] == 0x74 && data[6] == 0x79 && data[7] == 0x70 {
		brand := string(data[8:12])
		if brand == "avif" || brand == "avis" || brand == "mif1" {
			return ".avif", "image/avif"
		}
	}

	return "", ""
}

// SavePastedImage saves raw image data to .ledit/pasted-images/ in the current
// working directory. It returns a relative path like
// "./.ledit/pasted-images/paste_20260320_145959_abc123.png".
func SavePastedImage(data []byte) (string, error) {
	if len(data) > MaxPastedImageSize {
		return "", fmt.Errorf("pasted image exceeds maximum size of %d bytes", MaxPastedImageSize)
	}

	ext, _ := DetectImageMagic(data)
	if ext == "" {
		return "", fmt.Errorf("cannot determine image format for saved file")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	dir := filepath.Join(cwd, PastedImageDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create image directory: %w", err)
	}

	// Generate a unique filename: paste_YYYYMMDD_HHMMSS_<6hex>.<ext>
	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random suffix: %w", err)
	}
	suffix := hex.EncodeToString(randBytes)
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("paste_%s_%s%s", timestamp, suffix, ext)

	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to write image file: %w", err)
	}

	relativePath := "./" + filepath.Join(PastedImageDirName, filename)
	return relativePath, nil
}
