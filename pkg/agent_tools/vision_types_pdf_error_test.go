package tools

import "testing"

type pdfErr string

func (e pdfErr) Error() string { return string(e) }

func TestClassifyPDFProcessingErrorCode_VisionRequestFailure(t *testing.T) {
	err := pdfErr(`PDF OCR failed: OCR request failed: HTTP 500: {"error":{"message":"upstream failed"}}`)
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeVisionRequestFailed {
		t.Fatalf("expected %s, got %s", ErrCodeVisionRequestFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_InvalidPDFInput(t *testing.T) {
	err := pdfErr("input is not a valid PDF file (missing %PDF header)")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeInputUnsupported {
		t.Fatalf("expected %s, got %s", ErrCodeInputUnsupported, got)
	}
}

func TestClassifyPDFProcessingErrorCode_RemoteFetchFailure(t *testing.T) {
	err := pdfErr("PDF OCR failed: failed to download PDF: status 404")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeRemoteFetchFailed {
		t.Fatalf("expected %s, got %s", ErrCodeRemoteFetchFailed, got)
	}
}
