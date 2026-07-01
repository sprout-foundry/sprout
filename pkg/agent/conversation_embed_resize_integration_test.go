package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// Integration tests for readImageAsImageData (full pipeline)
// ---------------------------------------------------------------------------

// encodeJPEG creates a JPEG-encoded []byte from the given image at quality 90.
func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return buf.Bytes()
}

// writeTempImage writes image bytes to a new temp file and returns the path.
// The caller must os.RemoveAll(path) after use.
func writeTempImage(t *testing.T, data []byte, ext string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "img-*"+ext)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// base64Decode decodes a base64 string and returns the raw bytes.
func base64Decode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return b
}

// ---------------------------------------------------------------------------
// SHOULD_FIX #1 — Integration tests for readImageAsImageData
// ---------------------------------------------------------------------------

func TestReadImageAsImageData_LargePNG_Resized(t *testing.T) {
	// 2400x1800 PNG — long edge 2400 > 1568, should be resized.
	img := image.NewRGBA(image.Rect(0, 0, 2400, 1800))
	data := encodePNG(t, img)
	path := writeTempImage(t, data, ".png")

	got, size, err := readImageAsImageData(path)
	if err != nil {
		t.Fatalf("readImageAsImageData: %v", err)
	}
	if size == 0 {
		t.Fatal("expected non-zero size")
	}

	// Decode the returned base64 and check dimensions.
	decoded := base64Decode(t, got.Base64)
	resized, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode returned JPEG: %v", err)
	}
	bounds := resized.Bounds()
	longEdge := bounds.Dx()
	if bounds.Dy() > longEdge {
		longEdge = bounds.Dy()
	}
	if longEdge > 1568 {
		t.Errorf("long edge %d exceeds 1568px limit", longEdge)
	}

	// MIME type should be JPEG (resize path re-encodes as JPEG).
	if got.Type != "image/jpeg" {
		t.Errorf("type = %q, want %q", got.Type, "image/jpeg")
	}
}

func TestReadImageAsImageData_SmallPNG_NoOp(t *testing.T) {
	// 400x300 PNG — well under 1568px.
	// OptimizeImageData re-encodes as JPEG (quality 90) regardless of size,
	// so the output won't be byte-equal to the original PNG.
	// The pre-resize step is a no-op for small images (long edge ≤1568).
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	data := encodePNG(t, img)
	path := writeTempImage(t, data, ".png")

	got, _, err := readImageAsImageData(path)
	if err != nil {
		t.Fatalf("readImageAsImageData: %v", err)
	}

	// OptimizeImageData re-encodes as JPEG, so the output is JPEG.
	if got.Type != "image/jpeg" {
		t.Errorf("type = %q, want %q", got.Type, "image/jpeg")
	}

	// Verify the output is a valid, decodable image with preserved dimensions.
	decoded := base64Decode(t, got.Base64)
	resized, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode returned image: %v", err)
	}
	bounds := resized.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 300 {
		t.Errorf("dimensions: got %dx%d, want 400x300", bounds.Dx(), bounds.Dy())
	}
}

func TestReadImageAsImageData_SmallJPEG_NoOp(t *testing.T) {
	// 500x400 JPEG — under 1568px, pre-resize step is a no-op.
	// OptimizeImageData re-encodes as JPEG (quality 90), so bytes won't be
	// identical to the original, but dimensions are preserved.
	img := image.NewRGBA(image.Rect(0, 0, 500, 400))
	data := encodeJPEG(t, img)
	path := writeTempImage(t, data, ".jpg")

	got, _, err := readImageAsImageData(path)
	if err != nil {
		t.Fatalf("readImageAsImageData: %v", err)
	}

	// MIME type should be JPEG.
	if got.Type != "image/jpeg" {
		t.Errorf("type = %q, want %q", got.Type, "image/jpeg")
	}

	// Verify the output is a valid, decodable JPEG with preserved dimensions.
	decoded := base64Decode(t, got.Base64)
	resized, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode returned image: %v", err)
	}
	bounds := resized.Bounds()
	if bounds.Dx() != 500 || bounds.Dy() != 400 {
		t.Errorf("dimensions: got %dx%d, want 500x400", bounds.Dx(), bounds.Dy())
	}
}

// ---------------------------------------------------------------------------
// SHOULD_FIX #2 — JPEG input through resize path
// ---------------------------------------------------------------------------

func TestReadImageAsImageData_LargeJPEG_Resized(t *testing.T) {
	// 2000x1500 JPEG — long edge 2000 > 1568, should be resized.
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	data := encodeJPEG(t, img)
	path := writeTempImage(t, data, ".jpg")

	got, size, err := readImageAsImageData(path)
	if err != nil {
		t.Fatalf("readImageAsImageData: %v", err)
	}
	if size == 0 {
		t.Fatal("expected non-zero size")
	}

	// Decode the returned base64 and verify it's a valid JPEG.
	decoded := base64Decode(t, got.Base64)
	resized, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode returned JPEG: %v", err)
	}

	bounds := resized.Bounds()
	longEdge := bounds.Dx()
	if bounds.Dy() > longEdge {
		longEdge = bounds.Dy()
	}
	if longEdge > 1568 {
		t.Errorf("long edge %d exceeds 1568px limit", longEdge)
	}

	// MIME type should be JPEG.
	if got.Type != "image/jpeg" {
		t.Errorf("type = %q, want %q", got.Type, "image/jpeg")
	}

	// Verify the output is a valid image (non-empty bounds).
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Error("resized image has zero dimensions")
	}
}

// ---------------------------------------------------------------------------
// SHOULD_FIX #3 — Extreme aspect ratio tests
// ---------------------------------------------------------------------------

func TestResizeImageForVisionEmbed_ExtremeAspectRatios(t *testing.T) {
	t.Run("extremely_wide_5000x200", func(t *testing.T) {
		// Expected: ~1568x63 (200 * 1568 / 5000 = 62.72 → 63)
		w, h := 5000, 200
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		data := encodePNG(t, img)

		got, err := resizeImageForVisionEmbed(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resized := decodeJPEG(t, got)
		bounds := resized.Bounds()
		gotW, gotH := bounds.Dx(), bounds.Dy()

		wantW := 1568
		wantH := 63 // 200 * 1568 / 5000 = 62.72 → 63
		if gotW != wantW {
			t.Errorf("width: got %d, want %d", gotW, wantW)
		}
		if gotH != wantH {
			t.Errorf("height: got %d, want %d", gotH, wantH)
		}
	})

	t.Run("extremely_tall_200x5000", func(t *testing.T) {
		// Expected: ~63x1568 (200 * 1568 / 5000 = 62.72 → 63)
		w, h := 200, 5000
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		data := encodePNG(t, img)

		got, err := resizeImageForVisionEmbed(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resized := decodeJPEG(t, got)
		bounds := resized.Bounds()
		gotW, gotH := bounds.Dx(), bounds.Dy()

		wantW := 63 // 200 * 1568 / 5000 = 62.72 → 63
		wantH := 1568
		if gotW != wantW {
			t.Errorf("width: got %d, want %d", gotW, wantW)
		}
		if gotH != wantH {
			t.Errorf("height: got %d, want %d", gotH, wantH)
		}
	})

	t.Run("pathologically_thin_10000x1", func(t *testing.T) {
		// Short edge calculation: 1 * 1568 / 10000 = 0.1568 → rounds to 0.
		// The code guards against this with min 1, so short edge should be ≥ 1.
		w, h := 10000, 1
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		data := encodePNG(t, img)

		got, err := resizeImageForVisionEmbed(data)
		if err != nil {
			t.Fatalf("unexpected error (must not panic): %v", err)
		}

		resized := decodeJPEG(t, got)
		bounds := resized.Bounds()
		gotW, gotH := bounds.Dx(), bounds.Dy()

		if gotW != 1568 {
			t.Errorf("long edge: got %d, want 1568", gotW)
		}
		if gotH < 1 {
			t.Errorf("short edge: got %d, want >= 1", gotH)
		}
	})
}

// ---------------------------------------------------------------------------
// Integration test: extreme aspect ratio through full pipeline
// ---------------------------------------------------------------------------

func TestIntegrationEmbed_ExtremeWide(t *testing.T) {
	// 5000x200 PNG through the full readImageAsImageData pipeline.
	img := image.NewRGBA(image.Rect(0, 0, 5000, 200))
	data := encodePNG(t, img)
	path := writeTempImage(t, data, ".png")

	got, _, err := readImageAsImageData(path)
	if err != nil {
		t.Fatalf("readImageAsImageData: %v", err)
	}

	decoded := base64Decode(t, got.Base64)
	resized, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode returned image: %v", err)
	}
	bounds := resized.Bounds()
	if bounds.Dx() > 1568 {
		t.Errorf("width %d exceeds 1568px limit", bounds.Dx())
	}
	if bounds.Dy() < 1 {
		t.Errorf("height %d is less than 1", bounds.Dy())
	}
}
