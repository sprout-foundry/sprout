package tools

import (
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

type stringErr string

func (e stringErr) Error() string { return string(e) }

func assertErrString(s string) error { return stringErr(s) }
