package tools

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyContentType_ImageMIME(t *testing.T) {
	tests := []struct {
		ct       string
		urlPath  string
		expected ResponseKind
	}{
		{"image/png", "/photo.png", ResponseKindImage},
		{"image/jpeg", "/photo.jpg", ResponseKindImage},
		{"image/gif", "/anim.gif", ResponseKindImage},
		{"image/webp", "/img.webp", ResponseKindImage},
		{"image/svg+xml", "/icon.svg", ResponseKindText}, // SVG is text-based
		{"image/bmp", "/pic.bmp", ResponseKindImage},
		{"image/avif", "/photo.avif", ResponseKindImage},
		{"application/pdf", "/doc.pdf", ResponseKindPDF},
		{"text/html", "/page", ResponseKindUnknown},
		{"application/json", "/data.json", ResponseKindUnknown},
		{"", "/photo.png", ResponseKindImage}, // empty CT falls back to path extension
	}
	for _, tt := range tests {
		got := ClassifyContentType(tt.ct, tt.urlPath)
		if got != tt.expected {
			t.Errorf("ClassifyContentType(%q, %q) = %v, want %v", tt.ct, tt.urlPath, got, tt.expected)
		}
	}
}

func TestClassifyContentType_WithCharset(t *testing.T) {
	got := ClassifyContentType("image/png; charset=utf-8", "/x.png")
	if got != ResponseKindImage {
		t.Errorf("expected image for content type with charset, got %v", got)
	}
}

func TestClassifyByPathExtension_URLs(t *testing.T) {
	tests := []struct {
		url      string
		expected ResponseKind
	}{
		{"https://example.com/photo.png", ResponseKindImage},
		{"https://example.com/doc.pdf", ResponseKindPDF},
		{"https://example.com/icon.svg", ResponseKindText}, // SVG → text
		{"https://example.com/video.mp4", ResponseKindUnknown},
		{"https://cdn.example.com/photo.png?w=800", ResponseKindImage},
		{"https://cdn.example.com/doc.pdf#page=5", ResponseKindPDF},
		{"https://example.com/image.jpeg", ResponseKindImage},
		{"https://example.com/gif/anim.gif", ResponseKindImage},
		{"https://example.com/photo.webp", ResponseKindImage},
		{"https://example.com/photo.avif", ResponseKindImage},
		{"/local/path/file.png", ResponseKindImage},          // local path, no scheme prefix
		{"/local/path/file.pdf", ResponseKindPDF},
	}
	for _, tt := range tests {
		got := classifyByPathExtension(tt.url)
		if got != tt.expected {
			t.Errorf("classifyByPathExtension(%q) = %v, want %v", tt.url, got, tt.expected)
		}
	}
}

func TestResponseKindIsBinary(t *testing.T) {
	if ResponseKindUnknown.IsBinary() {
		t.Error("Unknown should not be binary")
	}
	if ResponseKindText.IsBinary() {
		t.Error("Text should not be binary")
	}
	if !ResponseKindImage.IsBinary() {
		t.Error("Image should be binary")
	}
	if !ResponseKindPDF.IsBinary() {
		t.Error("PDF should be binary")
	}
}

func TestResponseKindString(t *testing.T) {
	tests := []struct {
		kind ResponseKind
		want string
	}{
		{ResponseKindUnknown, "unknown"},
		{ResponseKindText, "text"},
		{ResponseKindImage, "image"},
		{ResponseKindPDF, "pdf"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ResponseKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestProbeURLContentType_ImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "image/png")
			return
		}
		// GET path
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}) // minimal PNG header
	}))
	defer server.Close()

	kind, effectiveURL := ProbeURLContentType(server.URL + "/photo.png")
	if kind != ResponseKindImage {
		t.Errorf("expected Image, got %v", kind)
	}
	if effectiveURL == "" {
		t.Error("expected non-empty effective URL")
	}
}

func TestProbeURLContentType_PDFURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "application/pdf")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	}))
	defer server.Close()

	kind, _ := ProbeURLContentType(server.URL + "/doc.pdf")
	if kind != ResponseKindPDF {
		t.Errorf("expected PDF, got %v", kind)
	}
}

func TestProbeURLContentType_HTMLURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}))
	defer server.Close()

	kind, _ := ProbeURLContentType(server.URL + "/page")
	if kind.IsBinary() {
		t.Errorf("HTML URL should not be binary, got %v", kind)
	}
}

func TestProbeURLContentType_FailsToConnect(t *testing.T) {
	// Use a port that's almost certainly not listening
	kind, url := ProbeURLContentType("http://127.0.0.1:1/nonexistent.png")
	if kind != ResponseKindImage {
		t.Errorf("expected fallback to extension-based Image classification, got %v", kind)
	}
	if url != "http://127.0.0.1:1/nonexistent.png" {
		t.Errorf("expected original URL on HEAD failure, got %s", url)
	}
}

func TestProcessImageBinary_ValidPNG(t *testing.T) {
	// Minimal valid PNG data
	pngData := []byte{
		0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R', // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1 pixel
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // 8-bit RGB
		0xDE, 0x00, 0x00, 0x00, 0x0C, 'I', 'D', 'A', // IDAT chunk
		0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, 0x00, // compressed data
		0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC, 0x33, // extra
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', // IEND chunk
		0xAE, 0x42, 0x60, 0x82,
	}

	result, err := processImageBinary("https://example.com/test.png", pngData)
	if err != nil {
		t.Fatalf("processImageBinary failed: %v", err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(result.Images))
	}
	if result.Images[0].Base64 == "" {
		t.Error("expected non-empty base64 data")
	}
	if result.Images[0].Type != "image/png" {
		t.Errorf("expected image/png type, got %s", result.Images[0].Type)
	}
	if result.Source != "downloaded_image" {
		t.Errorf("expected source 'downloaded_image', got %s", result.Source)
	}
}

func TestProcessImageBinary_InvalidData(t *testing.T) {
	_, err := processImageBinary("https://example.com/not-image.txt", []byte("not an image"))
	if err == nil {
		t.Error("expected error for non-image data")
	}
}

func TestProcessImageBinary_EmptyData(t *testing.T) {
	_, err := processImageBinary("https://example.com/empty.png", []byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestProcessPDFBinary_InvalidData(t *testing.T) {
	_, err := processPDFBinary("https://example.com/not-a-pdf.bin", []byte("not a pdf"))
	if err == nil {
		t.Error("expected error for non-PDF data")
	}
}

func TestProcessPDFBinary_EmptyData(t *testing.T) {
	_, err := processPDFBinary("https://example.com/empty.pdf", []byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestFetchBinaryURL_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := FetchBinaryURL(server.URL+"/image.png", ResponseKindImage)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestFetchBinaryURL_UnsupportedKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("text data"))
	}))
	defer server.Close()

	_, err := FetchBinaryURL(server.URL, ResponseKindUnknown)
	if err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestProcessImageBinary_EffectiveURLTracking(t *testing.T) {
	pngData := []byte{
		0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 'I', 'D', 'A',
		0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC, 0x33,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D',
		0xAE, 0x42, 0x60, 0x82,
	}

	result, err := processImageBinary("https://example.com/original.png", pngData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedURL := "https://example.com/original.png"
	if result.EffectiveURL != expectedURL {
		t.Errorf("EffectiveURL = %q, want %q", result.EffectiveURL, expectedURL)
	}
}
