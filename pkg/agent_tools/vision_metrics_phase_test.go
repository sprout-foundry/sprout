//go:build !js

package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// IncVisionFailure — per-reason counter tests
// ---------------------------------------------------------------------------

func TestIncVisionFailure_SingleReason(t *testing.T) {
	before := GetVisionMetrics()

	IncVisionFailure("http_5xx")

	after := GetVisionMetrics()
	if after.FailuresByReason["http_5xx"]-before.FailuresByReason["http_5xx"] != 1 {
		t.Errorf("http_5xx count = %d, want 1", after.FailuresByReason["http_5xx"])
	}
}

func TestIncVisionFailure_MultipleReasons(t *testing.T) {
	before := GetVisionMetrics()

	IncVisionFailure("http_5xx")
	IncVisionFailure("http_5xx")
	IncVisionFailure("http_429")
	IncVisionFailure("network")

	after := GetVisionMetrics()
	if after.FailuresByReason["http_5xx"]-before.FailuresByReason["http_5xx"] != 2 {
		t.Errorf("http_5xx count = %d, want 2", after.FailuresByReason["http_5xx"])
	}
	if after.FailuresByReason["http_429"]-before.FailuresByReason["http_429"] != 1 {
		t.Errorf("http_429 count = %d, want 1", after.FailuresByReason["http_429"])
	}
	if after.FailuresByReason["network"]-before.FailuresByReason["network"] != 1 {
		t.Errorf("network count = %d, want 1", after.FailuresByReason["network"])
	}
}

func TestIncVisionFailure_Concurrent(t *testing.T) {
	before := GetVisionMetrics()

	const goroutines = 8
	const incsPer = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incsPer; j++ {
				IncVisionFailure("unknown")
			}
		}()
	}
	wg.Wait()

	after := GetVisionMetrics()
	want := int64(goroutines * incsPer)
	got := after.FailuresByReason["unknown"] - before.FailuresByReason["unknown"]
	if got != want {
		t.Errorf("unknown failures: got delta %d, want %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// VisionMetricsSnapshot — new fields present
// ---------------------------------------------------------------------------

func TestVisionMetricsSnapshot_NewFields(t *testing.T) {
	before := GetVisionMetrics()

	IncVisionRetry()
	IncVisionRetry()
	IncVisionFallbackTotal()
	IncVisionFallbackSuccess()
	AddVisionLatencyRequest(100 * time.Millisecond)
	AddVisionLatencyRetrySleep(50 * time.Millisecond)
	AddVisionLatencyFallback(200 * time.Millisecond)
	AddVisionLatencyParse(10 * time.Millisecond)

	after := GetVisionMetrics()

	if after.RetryCount-before.RetryCount != 2 {
		t.Errorf("RetryCount delta = %d, want 2", after.RetryCount-before.RetryCount)
	}
	if after.OCRFallbackTotal-before.OCRFallbackTotal != 1 {
		t.Errorf("OCRFallbackTotal delta = %d, want 1", after.OCRFallbackTotal-before.OCRFallbackTotal)
	}
	if after.OCRFallbackSuccess-before.OCRFallbackSuccess != 1 {
		t.Errorf("OCRFallbackSuccess delta = %d, want 1", after.OCRFallbackSuccess-before.OCRFallbackSuccess)
	}
	if after.LatencyRequestMS-before.LatencyRequestMS != 100 {
		t.Errorf("LatencyRequestMS delta = %d, want 100", after.LatencyRequestMS-before.LatencyRequestMS)
	}
	if after.LatencyRetrySleepMS-before.LatencyRetrySleepMS != 50 {
		t.Errorf("LatencyRetrySleepMS delta = %d, want 50", after.LatencyRetrySleepMS-before.LatencyRetrySleepMS)
	}
	if after.LatencyFallbackMS-before.LatencyFallbackMS != 200 {
		t.Errorf("LatencyFallbackMS delta = %d, want 200", after.LatencyFallbackMS-before.LatencyFallbackMS)
	}
	if after.LatencyParseMS-before.LatencyParseMS != 10 {
		t.Errorf("LatencyParseMS delta = %d, want 10", after.LatencyParseMS-before.LatencyParseMS)
	}
	if after.FailuresByReason == nil {
		t.Error("FailuresByReason should not be nil in snapshot")
	}
}

// ---------------------------------------------------------------------------
// VisionMetricsSink — JSONL append tests
// ---------------------------------------------------------------------------

func TestAppendVisionRecord_WritesToSink(t *testing.T) {
	// Create a temp file and install a real visionMetricsSink pointing at it.
	tmpFile, err := os.CreateTemp("", "vision_metrics_test_*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	sink := &visionMetricsSink{
		file:   nil, // will be opened below
		writer: nil,
		path:   tmpPath,
	}
	// Open the file for append.
	f, err := os.OpenFile(tmpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	sink.file = f
	sink.writer = bufio.NewWriter(f)

	setVisionMetricsSinkForTesting(sink)
	defer func() {
		sink.Close()
		setVisionMetricsSinkForTesting(nil)
	}()

	rec := VisionMetricsRecord{
		Timestamp:           "2025-01-01T00:00:00Z",
		OpName:              "analyze_image",
		ImageCount:          1,
		Success:             true,
		RetryCount:          0,
		LatencyRequestMS:    150,
		LatencyRetrySleepMS: 0,
		LatencyParseMS:      5,
		ImageTokens:         256,
		ImageTokensCached:   128,
	}
	AppendVisionRecord(rec)

	// Flush and read the file.
	sink.mu.Lock()
	_ = sink.writer.Flush()
	sink.mu.Unlock()

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the record is in the file.
	var got VisionMetricsRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("failed to unmarshal record: %v", err)
	}
	if got.OpName != rec.OpName {
		t.Errorf("OpName = %q, want %q", got.OpName, rec.OpName)
	}
	if got.Success != rec.Success {
		t.Errorf("Success = %v, want %v", got.Success, rec.Success)
	}
	if got.ImageTokens != rec.ImageTokens {
		t.Errorf("ImageTokens = %d, want %d", got.ImageTokens, rec.ImageTokens)
	}
	if got.ImageTokensCached != rec.ImageTokensCached {
		t.Errorf("ImageTokensCached = %d, want %d", got.ImageTokensCached, rec.ImageTokensCached)
	}
}

func TestAppendVisionRecord_NilSink(t *testing.T) {
	// Set nil sink — AppendVisionRecord should not panic.
	setVisionMetricsSinkForTesting(nil)

	AppendVisionRecord(VisionMetricsRecord{
		OpName:     "test",
		ImageCount: 1,
		Success:    true,
	})
	// If we got here without panic, the test passes.
}

func TestVisionMetricsRecord_JSONFields(t *testing.T) {
	rec := VisionMetricsRecord{
		Timestamp:           "2025-01-01T00:00:00Z",
		SessionID:           "sess-123",
		OpName:              "analyze_image",
		ImageCount:          2,
		Success:             false,
		FailureReason:       "http_5xx",
		RetryCount:          2,
		UsedOCRFallback:     true,
		OCRFallbackSuccess:  false,
		LatencyRequestMS:    500,
		LatencyRetrySleepMS: 300,
		LatencyFallbackMS:   150,
		LatencyParseMS:      10,
		ImageTokens:         1024,
		ImageTokensCached:   512,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Verify key fields are present in JSON output.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if raw["op_name"] != "analyze_image" {
		t.Errorf("op_name = %v, want analyze_image", raw["op_name"])
	}
	if raw["image_count"] != float64(2) {
		t.Errorf("image_count = %v, want 2", raw["image_count"])
	}
	if raw["success"] != false {
		t.Errorf("success = %v, want false", raw["success"])
	}
	if raw["failure_reason"] != "http_5xx" {
		t.Errorf("failure_reason = %v, want http_5xx", raw["failure_reason"])
	}
	if raw["retry_count"] != float64(2) {
		t.Errorf("retry_count = %v, want 2", raw["retry_count"])
	}
	if raw["used_ocr_fallback"] != true {
		t.Errorf("used_ocr_fallback = %v, want true", raw["used_ocr_fallback"])
	}
	if raw["latency_request_ms"] != float64(500) {
		t.Errorf("latency_request_ms = %v, want 500", raw["latency_request_ms"])
	}
} // ---------------------------------------------------------------------------
// classifyVisionError — error classification tests
// ---------------------------------------------------------------------------

func TestClassifyVisionError_HTTP5xx(t *testing.T) {
	err := errors.New("HTTP 500: Internal Server Error")
	got := classifyVisionError(err)
	if got != "http_5xx" {
		t.Errorf("classifyVisionError(HTTP 500) = %q, want %q", got, "http_5xx")
	}
}

func TestClassifyVisionError_HTTP429(t *testing.T) {
	err := errors.New("HTTP 429: Too Many Requests")
	got := classifyVisionError(err)
	if got != "http_429" {
		t.Errorf("classifyVisionError(HTTP 429) = %q, want %q", got, "http_429")
	}
}

func TestClassifyVisionError_HTTP4xx(t *testing.T) {
	err := errors.New("HTTP 404: Not Found")
	got := classifyVisionError(err)
	if got != "http_4xx" {
		t.Errorf("classifyVisionError(HTTP 404) = %q, want %q", got, "http_4xx")
	}
}

func TestClassifyVisionError_ContextCanceled(t *testing.T) {
	got := classifyVisionError(context.Canceled)
	if got != "context_cancel" {
		t.Errorf("classifyVisionError(context.Canceled) = %q, want %q", got, "context_cancel")
	}
}

func TestClassifyVisionError_ContextDeadlineExceeded(t *testing.T) {
	got := classifyVisionError(context.DeadlineExceeded)
	if got != "context_cancel" {
		t.Errorf("classifyVisionError(context.DeadlineExceeded) = %q, want %q", got, "context_cancel")
	}
}

func TestClassifyVisionError_NetworkError(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	got := classifyVisionError(err)
	if got != "network" {
		t.Errorf("classifyVisionError(net.OpError) = %q, want %q", got, "network")
	}
}

func TestClassifyVisionError_Timeout(t *testing.T) {
	got := classifyVisionError(syscall.ETIMEDOUT)
	if got != "timeout" {
		t.Errorf("classifyVisionError(syscall.ETIMEDOUT) = %q, want %q", got, "timeout")
	}
}

func TestClassifyVisionError_ConnectionReset(t *testing.T) {
	got := classifyVisionError(syscall.ECONNRESET)
	if got != "network" {
		t.Errorf("classifyVisionError(syscall.ECONNRESET) = %q, want %q", got, "network")
	}
}

func TestClassifyVisionError_EOF(t *testing.T) {
	got := classifyVisionError(io.EOF)
	if got != "network" {
		t.Errorf("classifyVisionError(io.EOF) = %q, want %q", got, "network")
	}
}

func TestClassifyVisionError_InvalidResponse(t *testing.T) {
	err := errors.New("no response from vision model")
	got := classifyVisionError(err)
	if got != "invalid_response" {
		t.Errorf("classifyVisionError(no response) = %q, want %q", got, "invalid_response")
	}
}

func TestClassifyVisionError_Unknown(t *testing.T) {
	err := errors.New("some random error")
	got := classifyVisionError(err)
	if got != "unknown" {
		t.Errorf("classifyVisionError(random) = %q, want %q", got, "unknown")
	}
}

func TestClassifyVisionError_Nil(t *testing.T) {
	got := classifyVisionError(nil)
	if got != "unknown" {
		t.Errorf("classifyVisionError(nil) = %q, want %q", got, "unknown")
	}
}

// ---------------------------------------------------------------------------
// DoVisionRetry instrumentation — retry counter and stats
// ---------------------------------------------------------------------------

func TestDoVisionRetry_InstrumentsRetryCount(t *testing.T) {
	before := GetVisionMetrics()

	attempts := 0
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return &RetryableHTTPError{StatusCode: 500, Status: "Internal Server Error"}
		}
		return nil
	}, RetryOptions{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		JitterPct:   0,
		OpName:      "test_retry",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}

	after := GetVisionMetrics()
	// 2 retries (attempts 1 and 2 failed, attempt 3 succeeded)
	if after.RetryCount-before.RetryCount != 2 {
		t.Errorf("RetryCount delta = %d, want 2", after.RetryCount-before.RetryCount)
	}
}

func TestDoVisionRetry_InstrumentsFailure(t *testing.T) {
	before := GetVisionMetrics()

	attempts := 0
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		attempts++
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		MaxAttempts: 2,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		JitterPct:   0,
		OpName:      "test_failure",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	after := GetVisionMetrics()
	if after.FailuresByReason["http_5xx"]-before.FailuresByReason["http_5xx"] != 1 {
		t.Errorf("http_5xx failures delta = %d, want 1",
			after.FailuresByReason["http_5xx"]-before.FailuresByReason["http_5xx"])
	}
}

func TestDoVisionRetry_PopulatesRetryStats(t *testing.T) {
	var stats RetryStats
	attempts := 0

	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return &RetryableHTTPError{StatusCode: 500, Status: "Internal Server Error"}
		}
		return nil
	}, RetryOptions{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		JitterPct:   0,
		OpName:      "test_stats",
		Stats:       &stats,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.RetryCount != 2 {
		t.Errorf("RetryStats.RetryCount = %d, want 2", stats.RetryCount)
	}
	if stats.SleepDuration <= 0 {
		t.Errorf("RetryStats.SleepDuration = %v, want > 0", stats.SleepDuration)
	}
	if stats.LastError != nil {
		t.Errorf("RetryStats.LastError should be nil on success, got %v", stats.LastError)
	}
}

func TestDoVisionRetry_PopulatesRetryStats_OnFailure(t *testing.T) {
	var stats RetryStats

	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		return errors.New("HTTP 400: Bad Request")
	}, RetryOptions{
		MaxAttempts: 1,
		OpName:      "test_stats_fail",
		Stats:       &stats,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if stats.RetryCount != 0 {
		t.Errorf("RetryStats.RetryCount = %d, want 0 (non-retryable error)", stats.RetryCount)
	}
	if stats.LastError == nil {
		t.Error("RetryStats.LastError should be non-nil on failure")
	}
}

// ---------------------------------------------------------------------------
// InstrumentedVisionCall — synthetic end-to-end record test
// ---------------------------------------------------------------------------

// makeTestSink creates a real visionMetricsSink pointing at a temp file
// for testing. Returns the sink and a cleanup function.
func makeTestSink(t *testing.T) (*visionMetricsSink, func()) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "vision_metrics_test_*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	f, err := os.OpenFile(tmpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	sink := &visionMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   tmpPath,
	}
	setVisionMetricsSinkForTesting(sink)
	return sink, func() {
		sink.Close()
		os.Remove(tmpPath)
		setVisionMetricsSinkForTesting(nil)
	}
}

// readSinkRecords reads all VisionMetricsRecords from a sink's file.
func readSinkRecords(t *testing.T, sink *visionMetricsSink) []VisionMetricsRecord {
	t.Helper()
	sink.mu.Lock()
	_ = sink.writer.Flush()
	sink.mu.Unlock()

	data, err := os.ReadFile(sink.path)
	if err != nil {
		t.Fatal(err)
	}

	var records []VisionMetricsRecord
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var rec VisionMetricsRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("failed to unmarshal record: %v", err)
		}
		records = append(records, rec)
	}
	return records
}

// TestInstrumentedVisionCall_Synthetic verifies that a synthetic vision call
// (simulating AnalyzeImage instrumentation) produces a correct record.
func TestInstrumentedVisionCall_Synthetic(t *testing.T) {
	sink, cleanup := makeTestSink(t)
	defer cleanup()

	// Simulate a successful call with 1 retry and no fallback.
	rec := VisionMetricsRecord{
		Timestamp:           "2025-01-01T00:00:00Z",
		OpName:              "analyze_image",
		ImageCount:          1,
		Success:             true,
		RetryCount:          1,
		UsedOCRFallback:     false,
		OCRFallbackSuccess:  false,
		LatencyRequestMS:    250,
		LatencyRetrySleepMS: 200,
		LatencyFallbackMS:   0,
		LatencyParseMS:      8,
		ImageTokens:         512,
		ImageTokensCached:   256,
	}
	AppendVisionRecord(rec)

	records := readSinkRecords(t, sink)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	got := records[0]
	if !got.Success {
		t.Error("expected success=true")
	}
	if got.RetryCount != 1 {
		t.Errorf("expected retry_count=1, got %d", got.RetryCount)
	}
	if got.UsedOCRFallback {
		t.Error("expected used_ocr_fallback=false")
	}
	if got.LatencyRequestMS != 250 {
		t.Errorf("expected latency_request_ms=250, got %d", got.LatencyRequestMS)
	}
	if got.ImageTokens != 512 {
		t.Errorf("expected image_tokens=512, got %d", got.ImageTokens)
	}
}

// TestInstrumentedVisionCall_FallbackSuccess simulates a call where the
// primary vision model failed but OCR fallback succeeded.
func TestInstrumentedVisionCall_FallbackSuccess(t *testing.T) {
	sink, cleanup := makeTestSink(t)
	defer cleanup()

	rec := VisionMetricsRecord{
		Timestamp:           "2025-01-01T00:00:00Z",
		OpName:              "analyze_image",
		ImageCount:          1,
		Success:             true,
		FailureReason:       "",
		RetryCount:          2,
		UsedOCRFallback:     true,
		OCRFallbackSuccess:  true,
		LatencyRequestMS:    800,
		LatencyRetrySleepMS: 400,
		LatencyFallbackMS:   150,
		LatencyParseMS:      5,
	}
	AppendVisionRecord(rec)

	records := readSinkRecords(t, sink)
	got := records[0]
	if !got.Success {
		t.Error("expected success=true (fallback succeeded)")
	}
	if !got.OCRFallbackSuccess {
		t.Error("expected ocr_fallback_success=true")
	}
	if got.LatencyFallbackMS != 150 {
		t.Errorf("expected latency_fallback_ms=150, got %d", got.LatencyFallbackMS)
	}
}

// TestInstrumentedVisionCall_FailureWithFallback simulates a call where
// both primary and OCR fallback failed.
func TestInstrumentedVisionCall_FailureWithFallback(t *testing.T) {
	sink, cleanup := makeTestSink(t)
	defer cleanup()

	rec := VisionMetricsRecord{
		Timestamp:           "2025-01-01T00:00:00Z",
		OpName:              "analyze_image",
		ImageCount:          1,
		Success:             false,
		FailureReason:       "http_5xx",
		RetryCount:          2,
		UsedOCRFallback:     true,
		OCRFallbackSuccess:  false,
		LatencyRequestMS:    900,
		LatencyRetrySleepMS: 500,
		LatencyFallbackMS:   200,
	}
	AppendVisionRecord(rec)

	records := readSinkRecords(t, sink)
	got := records[0]
	if got.Success {
		t.Error("expected success=false")
	}
	if got.FailureReason != "http_5xx" {
		t.Errorf("expected failure_reason=http_5xx, got %q", got.FailureReason)
	}
	if !got.UsedOCRFallback {
		t.Error("expected used_ocr_fallback=true")
	}
}
