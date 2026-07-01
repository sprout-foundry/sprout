package tools

import (
	"strconv"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	visionMaxImageFileSizeBytes    = 20 * 1024 * 1024 // 20MB hard cap (API limit)
	visionOptimizeThresholdBytes   = 8 * 1024 * 1024  // Try optimization above 8MB
	visionTargetOptimizedSizeBytes = 2 * 1024 * 1024  // Target ~2MB for vision API calls
	visionMaxOptimizedJPEGQuality  = 85
	visionMinOptimizedJPEGQuality  = 35
	visionOptimizedJPEGQualityStep = 10
	visionResizeStepPercent        = 0.8   // Resize to 80% of original dimensions each iteration
	visionMinDimension             = 256   // Minimum dimension in pixels
	visionMaxDimension             = 4096  // Maximum pixels on longest edge for vision models
	visionMaxReturnedTextChars     = 20000 // Raised from 12000 to 20000 for better PDF/doc coverage
)

// ============================================================================
// Error Codes
// ============================================================================

// Error codes for analyze_image_content tool
const (
	ErrCodeInputUnsupported    = "INPUT_UNSUPPORTED_TYPE"
	ErrCodeRemoteFetchFailed   = "REMOTE_FETCH_FAILED"
	ErrCodeLocalFileNotFound   = "LOCAL_FILE_NOT_FOUND"
	ErrCodeOCRNoTextDetected   = "OCR_NO_TEXT_DETECTED"
	ErrCodePDFProcessingFailed = "PDF_PROCESSING_FAILED"
	ErrCodeVisionNotAvailable  = "VISION_NOT_AVAILABLE"
	ErrCodeVisionRequestFailed = "VISION_REQUEST_FAILED"
	ErrCodeInvalidResponse     = "INVALID_RESPONSE"

	// Special error for model download needed
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

// VisionUsageInfo contains token usage and cost information from vision model calls
type VisionUsageInfo struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// VisionProcessor handles image analysis using vision-capable models
type VisionProcessor struct {
	visionClient api.ClientInterface
	logger       *utils.Logger
	debug        bool
	usage        *VisionUsageInfo // Per-session usage tracking (SP-103-A4)
}

// ============================================================================
// Caching and Usage Tracking
// ============================================================================

// visionLastUsageMirror provides thread-safe access to the most recent
// vision usage info across all VisionProcessor sessions.
type visionLastUsageMirror struct {
	mu    sync.RWMutex
	usage *VisionUsageInfo
}

var lastUsageMirror = &visionLastUsageMirror{}

// recordVisionUsage writes usage info to both the per-session processor
// and the cross-session global mirror. Call this from any place that
// previously wrote to the package-global lastVisionUsage.
//
// If vp is nil (e.g., cache hit before a processor is created), only the
// global mirror is updated. If usage is nil, nothing happens.
func recordVisionUsage(vp *VisionProcessor, usage *VisionUsageInfo) {
	if vp != nil {
		vp.usage = usage
	}
	if usage == nil {
		return
	}
	lastUsageMirror.mu.Lock()
	lastUsageMirror.usage = usage
	lastUsageMirror.mu.Unlock()
}

// GetLastVisionUsage returns the usage information from the most recent
// vision model call across all sessions. Thread-safe.
func GetLastVisionUsage() *VisionUsageInfo {
	lastUsageMirror.mu.RLock()
	defer lastUsageMirror.mu.RUnlock()
	return lastUsageMirror.usage
}

// ClearLastVisionUsage clears the stored vision usage information.
// Thread-safe.
func ClearLastVisionUsage() {
	lastUsageMirror.mu.Lock()
	lastUsageMirror.usage = nil
	lastUsageMirror.mu.Unlock()
}

// LastUsage returns the per-session usage info for this VisionProcessor.
// Returns nil if no vision call has been made with this processor yet.
func (vp *VisionProcessor) LastUsage() *VisionUsageInfo {
	return vp.usage
}

// getVisionMaxReturnedTextChars returns the max text chars limit from env or default
func getVisionMaxReturnedTextChars() int {
	if raw := configuration.GetEnvSimple("VISION_MAX_TEXT_CHARS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return visionMaxReturnedTextChars
}

// GetVisionCacheStats returns statistics about vision result caching
func GetVisionCacheStats() map[string]interface{} {
	visionLRU.mu.Lock()
	defer visionLRU.mu.Unlock()

	stats := make(map[string]interface{})
	stats["cached_results"] = int(visionLRU.stats.Size.Load())
	stats["hits"] = int(visionLRU.stats.Hits.Load())
	stats["misses"] = int(visionLRU.stats.Misses.Load())
	stats["evictions"] = int(visionLRU.stats.Evictions.Load())
	stats["insertions"] = int(visionLRU.stats.Insertions.Load())
	stats["capacity"] = visionLRU.capacity

	// Compute estimated savings from cached usage info
	totalSavedCost := 0.0
	for _, e := range visionLRU.entries {
		if e.usage != nil {
			totalSavedCost += e.usage.EstimatedCost
		}
	}
	stats["estimated_savings"] = totalSavedCost

	return stats
}
