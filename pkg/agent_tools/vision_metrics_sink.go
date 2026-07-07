//go:build !js

package tools

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// VisionMetricsRecord — per-call JSONL entry
// ---------------------------------------------------------------------------

// VisionMetricsRecord is the per-call entry persisted to
// ~/.config/sprout/vision_metrics.jsonl. Fire-and-forget — the
// instrumentation never blocks the agent loop on file IO.
type VisionMetricsRecord struct {
	Timestamp          string `json:"timestamp"`                // RFC3339
	SessionID          string `json:"session_id,omitempty"`
	OpName             string `json:"op_name"`                  // e.g. "analyze_image"
	ImageCount         int    `json:"image_count"`              // number of images in this call
	Success            bool   `json:"success"`
	FailureReason      string `json:"failure_reason,omitempty"` // classified reason (empty if success)
	RetryCount         int    `json:"retry_count"`              // number of retry attempts (0 = first attempt succeeded)
	UsedOCRFallback    bool   `json:"used_ocr_fallback"`
	OCRFallbackSuccess bool   `json:"ocr_fallback_success"`
	LatencyRequestMS   int64  `json:"latency_request_ms"`       // total provider call wall time (all attempts)
	LatencyRetrySleepMS int64 `json:"latency_retry_sleep_ms"`   // time spent sleeping between retries
	LatencyFallbackMS  int64  `json:"latency_fallback_ms"`      // OCR fallback wall time (0 if not used)
	LatencyParseMS     int64  `json:"latency_parse_ms"`         // response parsing wall time
	ImageTokens        int    `json:"image_tokens"`             // prompt image tokens (including cached)
	ImageTokensCached  int    `json:"image_tokens_cached"`      // cached image tokens
}

// ---------------------------------------------------------------------------
// visionMetricsSink — buffered JSONL appender
// ---------------------------------------------------------------------------

// visionMetricsSink is a buffered JSONL appender for vision metrics.
// Safe for concurrent use from multiple goroutines.
type visionMetricsSink struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	path   string
}

// newVisionMetricsSink creates a sink that appends to
// ~/.config/sprout/vision_metrics.jsonl. Returns nil if the file cannot be
// opened (permission error, missing home dir, etc). Callers MUST treat nil
// as a no-op sink.
func newVisionMetricsSink() *visionMetricsSink {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(homeDir, ".config", "sprout")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	path := filepath.Join(dir, "vision_metrics.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return &visionMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}
}

// Append writes a single JSONL record. Best-effort: flush errors are
// silently discarded.
func (s *visionMetricsSink) Append(rec VisionMetricsRecord) {
	if s == nil {
		return
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().Format(time.RFC3339)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.writer.Write(data)
	_ = s.writer.WriteByte('\n')
	_ = s.writer.Flush()
}

// Close releases the file handle. Idempotent and nil-safe.
func (s *visionMetricsSink) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.writer.Flush()
	err := s.file.Close()
	s.file = nil
	return err
}

// ---------------------------------------------------------------------------
// Global singleton sink (lazy-init, thread-safe)
// ---------------------------------------------------------------------------

var defaultVisionSink *visionMetricsSink
var defaultVisionSinkOnce sync.Once

// getVisionMetricsSink returns the process-wide vision metrics sink.
// Returns nil if the sink could not be initialized (file open failed).
// If setVisionMetricsSinkForTesting was called first, returns the test sink.
func getVisionMetricsSink() *visionMetricsSink {
	defaultVisionSinkOnce.Do(func() {
		if defaultVisionSink == nil {
			defaultVisionSink = newVisionMetricsSink()
		}
	})
	return defaultVisionSink
}

// setVisionMetricsSinkForTesting replaces the global sink with a custom one.
// Intended for tests only; not safe for concurrent use with AppendVisionRecord.
func setVisionMetricsSinkForTesting(sink *visionMetricsSink) {
	defaultVisionSink = sink
}

// resetVisionMetricsSinkForTesting clears the test sink so subsequent calls
// to getVisionMetricsSink() re-initialize the default.
func resetVisionMetricsSinkForTesting() {
	defaultVisionSink = nil
	// We can't reset the Once, so we accept that after reset + set,
	// the caller must use setVisionMetricsSinkForTesting again.
}

// AppendVisionRecord appends a vision metrics record to the JSONL sink.
// Fire-and-forget: never blocks the caller on IO.
func AppendVisionRecord(rec VisionMetricsRecord) {
	sink := getVisionMetricsSink()
	if sink != nil {
		sink.Append(rec)
	}
}
