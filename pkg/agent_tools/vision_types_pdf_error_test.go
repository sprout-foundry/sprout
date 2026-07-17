package tools

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

type pdfErr string

func (e pdfErr) Error() string { return string(e) }

func TestClassifyPDFProcessingErrorCode_VisionRequestFailure(t *testing.T) {
	err := pdfErr(`PDF OCR: OCR request: HTTP 500: {"error":{"message":"upstream failed"}}`)
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
	err := pdfErr("PDF OCR: download PDF: status 404")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeRemoteFetchFailed {
		t.Fatalf("expected %s, got %s", ErrCodeRemoteFetchFailed, got)
	}
}

// ============================================================================
// SP-103-A9: Typed error classification tests
// ============================================================================

func TestClassifyPDFProcessingErrorCode_TypedNotFound(t *testing.T) {
	err := agenterrors.NewNotFound("foo.pdf")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeLocalFileNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeLocalFileNotFound, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedNetwork(t *testing.T) {
	err := agenterrors.NewNetwork("download PDF", errors.New("connection refused"))
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeRemoteFetchFailed {
		t.Fatalf("expected %s, got %s", ErrCodeRemoteFetchFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedValidation(t *testing.T) {
	err := agenterrors.NewValidation("missing %PDF header", nil)
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeInputUnsupported {
		t.Fatalf("expected %s, got %s", ErrCodeInputUnsupported, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedTimeout(t *testing.T) {
	err := agenterrors.NewTimeout("ocr request", 30*time.Second)
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeVisionRequestFailed {
		t.Fatalf("expected %s, got %s", ErrCodeVisionRequestFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedWrappedChain(t *testing.T) {
	// Typed error wrapped in a plain error chain via fmt.Errorf("...: %w", typedErr)
	typed := agenterrors.NewNotFound("foo.pdf")
	wrapped := fmt.Errorf("resolve PDF input path: %w", typed)
	if got := classifyPDFProcessingErrorCode(wrapped); got != ErrCodeLocalFileNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeLocalFileNotFound, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedNetworkWrappedChain(t *testing.T) {
	// Network error wrapped in an outer error
	inner := agenterrors.NewNetwork("download PDF", errors.New("connection refused"))
	wrapped := fmt.Errorf("resolve PDF input path: %w", inner)
	if got := classifyPDFProcessingErrorCode(wrapped); got != ErrCodeRemoteFetchFailed {
		t.Fatalf("expected %s, got %s", ErrCodeRemoteFetchFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedValidationWrappedChain(t *testing.T) {
	// Validation error wrapped in an outer error
	inner := agenterrors.NewValidation("downloaded content is not a valid PDF", nil)
	wrapped := fmt.Errorf("resolve PDF input path: %w", inner)
	if got := classifyPDFProcessingErrorCode(wrapped); got != ErrCodeInputUnsupported {
		t.Fatalf("expected %s, got %s", ErrCodeInputUnsupported, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedTool(t *testing.T) {
	err := agenterrors.NewTool("pdf_reader", "page render failed", errors.New("pypdfium2 error"))
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeVisionRequestFailed {
		t.Fatalf("expected %s, got %s", ErrCodeVisionRequestFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedNil(t *testing.T) {
	if got := classifyPDFProcessingErrorCode(nil); got != ErrCodePDFProcessingFailed {
		t.Fatalf("expected %s, got %s", ErrCodePDFProcessingFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_LegacyFallbackUntyped(t *testing.T) {
	// Untyped errors still work via the legacy fallback
	err := errors.New("download PDF: connection refused")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeRemoteFetchFailed {
		t.Fatalf("expected %s, got %s", ErrCodeRemoteFetchFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_LegacyFallbackNoSuchFile(t *testing.T) {
	err := errors.New("open test.pdf: no such file or directory")
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodeLocalFileNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeLocalFileNotFound, got)
	}
}

func TestClassifyPDFProcessingErrorCode_TypedNotInChain(t *testing.T) {
	// Untyped error with no TypedError in chain falls to legacy
	err := fmt.Errorf("resolve PDF input path: %w", errors.New("unknown error"))
	if got := classifyPDFProcessingErrorCode(err); got != ErrCodePDFProcessingFailed {
		t.Fatalf("expected %s, got %s", ErrCodePDFProcessingFailed, got)
	}
}

func TestClassifyPDFProcessingErrorCode_NotFoundCausePreserved(t *testing.T) {
	// Simulate an os.Stat error on a missing file
	_, statErr := os.Stat("/nonexistent/path/foo.pdf")
	if statErr == nil {
		t.Fatal("expected os.Stat to fail on /nonexistent/path/foo.pdf")
	}

	// Wrap it the way vision_pdf.go does
	wrapped := fmt.Errorf("stat PDF file: %w", agenterrors.NewNotFoundCause("/nonexistent/path/foo.pdf", statErr))

	// Typed-error path still classifies correctly
	if got := classifyPDFProcessingErrorCode(wrapped); got != ErrCodeLocalFileNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeLocalFileNotFound, got)
	}

	// And the original *PathError is still reachable via errors.As / errors.Is
	var pathErr *os.PathError
	if !errors.As(wrapped, &pathErr) {
		t.Fatal("expected *os.PathError to be reachable through the error chain")
	}
	if !os.IsNotExist(pathErr) {
		t.Errorf("expected pathErr to indicate a not-exist condition")
	}
}
