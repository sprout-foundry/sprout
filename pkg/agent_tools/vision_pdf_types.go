package tools

const (
	// visionMaxRemotePDFFileSizeBytes caps remote PDF downloads at 60MB.
	// Mirrors the 60MB io.LimitReader in downloadRemotePDFToTemp.
	visionMaxRemotePDFFileSizeBytes = 60 * 1024 * 1024
)

// Error code for PDF processing failures
const (
	ErrCodePDFProcessingFailed = "PDF_PROCESSING_FAILED"
)
