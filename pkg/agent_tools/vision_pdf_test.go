package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSelectImagesForOCR_ReturnsAllWhenUnderLimit(t *testing.T) {
	images := [][]byte{{1, 2}, {3}, {4, 5, 6}}
	selected := selectImagesForOCR(images, 5)
	if !reflect.DeepEqual(selected, images) {
		t.Fatalf("expected original image set when under limit")
	}
}

func TestSelectImagesForOCR_ChoosesLargestAndPreservesOriginalOrder(t *testing.T) {
	images := [][]byte{
		make([]byte, 5),  // index 0
		make([]byte, 20), // index 1
		make([]byte, 8),  // index 2
		make([]byte, 18), // index 3
		make([]byte, 1),  // index 4
	}

	selected := selectImagesForOCR(images, 2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected images, got %d", len(selected))
	}

	expected := [][]byte{images[1], images[3]}
	if !reflect.DeepEqual(selected, expected) {
		t.Fatalf("unexpected selected images")
	}
}

func TestCompactVisionError_RedactsDataURLAndTruncates(t *testing.T) {
	raw := "OCR request failed: Failed to process data:application/pdf;base64," + strings.Repeat("A", 2000)
	msg := compactVisionError(assertErrString(raw))
	if strings.Contains(msg, "base64,AAAA") {
		t.Fatalf("expected base64 payload to be redacted, got: %s", msg)
	}
	if !strings.Contains(msg, "base64,[REDACTED]") {
		t.Fatalf("expected redaction marker, got: %s", msg)
	}
	if len(msg) > 820 {
		t.Fatalf("expected truncated compact error, got length=%d", len(msg))
	}
}

func TestLooksLikePDF(t *testing.T) {
	if !looksLikePDF([]byte("%PDF-1.7\n...")) {
		t.Fatalf("expected valid PDF header to be detected")
	}
	if looksLikePDF([]byte("<!doctype html>")) {
		t.Fatalf("expected non-PDF content to be rejected")
	}
}

func TestResolvePDFInputPath_LocalPathPassthrough(t *testing.T) {
	path, cleanup, err := resolvePDFInputPath("/tmp/sample.pdf")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer cleanup()
	if path != "/tmp/sample.pdf" {
		t.Fatalf("expected path passthrough, got: %s", path)
	}
}

func TestResolvePDFInputPath_RemoteURLDownloadsToTemp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"))
	}))
	defer server.Close()

	path, cleanup, err := resolvePDFInputPath(server.URL + "/menu.pdf")
	if err != nil {
		t.Fatalf("expected remote PDF to resolve, got: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty temp path")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected temp file to exist: %v", statErr)
	}
	cleanup()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp file cleanup, stat err=%v", statErr)
	}
}

func TestResolvePDFInputPath_RemoteURLRejectsNonPDF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not a pdf</html>"))
	}))
	defer server.Close()

	_, cleanup, err := resolvePDFInputPath(server.URL + "/menu.pdf")
	defer cleanup()
	if err == nil {
		t.Fatal("expected non-PDF remote content to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not a valid pdf") {
		t.Fatalf("expected invalid PDF error, got: %v", err)
	}
}

type stringErr string

func (e stringErr) Error() string { return string(e) }

func assertErrString(s string) error { return stringErr(s) }
