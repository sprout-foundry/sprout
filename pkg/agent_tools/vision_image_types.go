package tools

const (
	visionMaxImageFileSizeBytes    = 20 * 1024 * 1024 // 20MB hard cap (API limit)
	visionOptimizeThresholdBytes   = 8 * 1024 * 1024  // Try optimization above 8MB
	visionTargetOptimizedSizeBytes = 2 * 1024 * 1024  // Target ~2MB for vision API calls
	visionMaxOptimizedJPEGQuality  = 85
	visionMinOptimizedJPEGQuality  = 35
	visionOptimizedJPEGQualityStep = 10
	visionResizeStepPercent        = 0.8  // Resize to 80% of original dimensions each iteration
	visionMinDimension             = 256  // Minimum dimension in pixels
	visionMaxDimension             = 4096 // Maximum pixels on longest edge for vision models
)

// Error codes for input and file handling
const (
	ErrCodeInputUnsupported    = "INPUT_UNSUPPORTED_TYPE"
	ErrCodeLocalFileNotFound   = "LOCAL_FILE_NOT_FOUND"
	ErrCodeModelDownloadNeeded = "MODEL_DOWNLOAD_NEEDED"
	ErrModelDownloadNeeded     = "PDF_OCR_MODEL_NEEDS_DOWNLOAD:"
)

// ============================================================================
// Data Structures
// ============================================================================

// VisionAnalysis represents the result of vision model analysis
type VisionAnalysis struct {
	ImagePath   string      `json:"image_path"`
	Description string      `json:"description"`
	Elements    []UIElement `json:"elements,omitempty"`
	Issues      []string    `json:"issues,omitempty"`
	Suggestions []string    `json:"suggestions,omitempty"`
}

// ImageAnalysisResponse represents a structured response for the analyze_image_content tool
type ImageAnalysisResponse struct {
	Success         bool                   `json:"success"`
	ToolInvoked     bool                   `json:"tool_invoked"`
	InputResolved   bool                   `json:"input_resolved"`
	OCRAttempted    bool                   `json:"ocr_attempted"`
	InputType       string                 `json:"input_type"` // "local_file", "remote_url", "unknown"
	InputPath       string                 `json:"input_path"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	ErrorMessage    string                 `json:"error_message,omitempty"`
	ExtractedText   string                 `json:"extracted_text,omitempty"`
	OutputTruncated bool                   `json:"output_truncated,omitempty"`
	OriginalChars   int                    `json:"original_chars,omitempty"`
	ReturnedChars   int                    `json:"returned_chars,omitempty"`
	FullOutputPath  string                 `json:"full_output_path,omitempty"` // Path to full OCR/analysis text when truncated
	Analysis        *VisionAnalysis        `json:"analysis,omitempty"`
	SupportedInput  ImageAnalysisSupported `json:"supported_input"`
}

// ImageAnalysisSupported describes what input types are supported
type ImageAnalysisSupported struct {
	RemoteURL     bool   `json:"remote_url"`
	LocalFile     bool   `json:"local_file"`
	ImageFormats  bool   `json:"image_formats"`  // jpg, png, gif, webp, etc.
	PDFSupport    bool   `json:"pdf_support"`    // PDF support status
	PDFWorkaround string `json:"pdf_workaround"` // Instructions for PDF handling
	MaxFileSizeMB int    `json:"max_file_size_mb"`
}

// UIElement represents a UI element detected in an image
type UIElement struct {
	Type        string `json:"type"`             // button, input, text, etc.
	Description string `json:"description"`      // what it looks like
	Position    string `json:"position"`         // approximate location
	Issues      string `json:"issues,omitempty"` // any problems noted
}
