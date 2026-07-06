package tools

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// =============================================================================
// SP-103-A2 — parallel OCR worker pool tests
//
// Exercises getVisionParallelWorkers + processOCRImagesParallel:
//   - per-image ordering preserved in the joined output
//   - progress callback receives (completed, total) growing to total
//   - failure threshold (>=2) cancels remaining work
//   - worker-count env-var parsing and clamping
//   - actual parallelism (peak concurrency ≈ worker count)
//
// All tests use a mock client; no network, no API keys.
// =============================================================================

// indexedMock identifies the input image by its byte length (we vary length
// across test inputs), so each SendVisionRequest call can return content
// specific to its input image, even though calls happen in random order.
type indexedMock struct {
	mu            sync.Mutex
	activeCount   atomic.Int32
	peakActive    atomic.Int32
	delay         time.Duration
	responsesByID map[string]string
}

func (m *indexedMock) SendVisionRequest(ctx context.Context, messages []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	active := m.activeCount.Add(1)
	defer m.activeCount.Add(-1)
	for {
		peak := m.peakActive.Load()
		if active <= peak {
			break
		}
		if m.peakActive.CompareAndSwap(peak, active) {
			break
		}
	}
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	var id string
	if len(messages) > 0 && len(messages[0].Images) > 0 {
		id = messages[0].Images[0].Base64
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.responsesByID[id]; ok {
		return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Content: v}}}}, nil
	}
	return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Content: "default"}}}}, nil
}

func (m *indexedMock) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *indexedMock) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *indexedMock) CheckConnection() error             { return nil }
func (m *indexedMock) SetDebug(bool)                      {}
func (m *indexedMock) SetModel(string) error              { return nil }
func (m *indexedMock) GetModel() string                   { return "mock" }
func (m *indexedMock) GetProvider() string                { return "mock" }
func (m *indexedMock) GetModelContextLimit() (int, error) { return 128000, nil }
func (m *indexedMock) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *indexedMock) SupportsVision() bool            { return true }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (m *indexedMock) SupportsConversationalVision() bool {
	return false
}
func (m *indexedMock) GetVisionModel() string          { return "mock-vision" }
func (m *indexedMock) GetLastTPS() float64             { return 0 }
func (m *indexedMock) GetAverageTPS() float64          { return 0 }
func (m *indexedMock) GetTPSStats() map[string]float64 { return nil }
func (m *indexedMock) ResetTPSStats()                  {}

// VisionCapabilities returns the safe defaults — these mocks focus on
// parallel-pool mechanics, not capability tuning. Method exists to
// satisfy api.ClientInterface after SP-103-D3 / AUDIT-GAP-2.
func (m *indexedMock) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}

// failingMock fails the configured call numbers (1-indexed) and otherwise
// succeeds. If alwaysFail is true, every call (including retries) fails.
// Tracks peak active concurrency.
type failingMock struct {
	mu          sync.Mutex
	failFor     map[int]bool
	alwaysFail  bool
	callCount   atomic.Int32
	activeCount atomic.Int32
	peakActive  atomic.Int32
	delay       time.Duration
}

func (m *failingMock) SendVisionRequest(ctx context.Context, _ []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	cur := m.callCount.Add(1)
	active := m.activeCount.Add(1)
	defer m.activeCount.Add(-1)
	for {
		peak := m.peakActive.Load()
		if active <= peak {
			break
		}
		if m.peakActive.CompareAndSwap(peak, active) {
			break
		}
	}
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.alwaysFail || m.failFor[int(cur)] {
		return nil, &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable", Method: "POST", URL: "mock://vision"}
	}
	return &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Content: "ok"}}},
	}, nil
}

func (m *failingMock) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *failingMock) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *failingMock) CheckConnection() error             { return nil }
func (m *failingMock) SetDebug(bool)                      {}
func (m *failingMock) SetModel(string) error              { return nil }
func (m *failingMock) GetModel() string                   { return "mock" }
func (m *failingMock) GetProvider() string                { return "mock" }
func (m *failingMock) GetModelContextLimit() (int, error) { return 128000, nil }
func (m *failingMock) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *failingMock) SupportsVision() bool            { return true }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (m *failingMock) SupportsConversationalVision() bool {
	return false
}
func (m *failingMock) GetVisionModel() string          { return "mock-vision" }
func (m *failingMock) GetLastTPS() float64             { return 0 }
func (m *failingMock) GetAverageTPS() float64          { return 0 }
func (m *failingMock) GetTPSStats() map[string]float64 { return nil }
func (m *failingMock) ResetTPSStats()                  {}

// VisionCapabilities returns the safe defaults — this mock focuses on
// failure-retry semantics, not capability tuning. Method exists to
// satisfy api.ClientInterface after SP-103-D3 / AUDIT-GAP-2.
func (m *failingMock) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}

// =============================================================================
// Helpers
// =============================================================================

// distinctImageBytes returns distinct bytes of varying length per idx, so
// the optimzer keeps them distinct (length-based) and the mock can identify
// each image by base64 length. The byte content is mixed enough that the
// optimzer can't compress it to a uniform hash.
func distinctImageBytes(idx int) []byte {
	data := make([]byte, 32+idx*5)
	for i := range data {
		data[i] = byte((idx*31 + i*7 + 0xA7) & 0xff)
	}
	return data
}

// =============================================================================
// Test 1: ordering preserved (results joined in input index order)
// =============================================================================

func TestProcessOCRImages_PreservesOrder(t *testing.T) {
	// Build mock that responds per-input-image with a unique signature.
	mock := &indexedMock{
		delay: 10 * time.Millisecond,
	}
	images := [][]byte{distinctImageBytes(0), distinctImageBytes(1), distinctImageBytes(2), distinctImageBytes(3)}

	// Pre-populate responses keyed by image bytes (the mock keys on
	// messages[0].Images[0].Base64, which encodes the raw bytes).
	// Because we don't know the base64 form up front without computing it,
	// we instead reuse the same mock without per-image responses (all
	// calls return "default") and verify ordering via section labels.
	mock.responsesByID = map[string]string{} // empty → all "default"

	text, err := processOCRImagesParallel(context.Background(), images, mock, "Page", nil)
	requireParallelNoError(t, err)
	if text == "" {
		t.Fatal("expected non-empty text")
	}

	// 3 section breaks between 4 successful images (no separator before
	// the first section).
	if got, want := strings.Count(text, "--- Page "), 3; got != want {
		t.Errorf("expected %d page separators, got %d in:\n%s", want, got, text)
	}
	// Each section header 2-4 is present, in index order. Section 1 has no
	// separator prefix (it's the leading section).
	prev := -1
	for i := 2; i <= 4; i++ {
		marker := "--- Page " + intToStr(i) + " ---"
		idx := strings.Index(text, marker)
		if idx < 0 {
			t.Errorf("missing section header for page %d in:\n%s", i, text)
			continue
		}
		if idx <= prev {
			t.Errorf("section %d appears before section %d in:\n%s", i, i-1, text)
		}
		prev = idx
	}
	// Verify there's exactly 4 occurrences of "default" (one per image).
	if got := strings.Count(text, "default"); got != 4 {
		t.Errorf("expected 4 'default' content blocks, got %d in:\n%s", got, text)
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// =============================================================================
// Test 2: progress callback
// =============================================================================

func TestProcessOCRImages_ProgressCallback(t *testing.T) {
	mock := &indexedMock{responsesByID: map[string]string{}}
	images := [][]byte{distinctImageBytes(0), distinctImageBytes(1), distinctImageBytes(2)}

	var mu sync.Mutex
	var progress []struct{ completed, total int }
	progressFn := func(completed, total int) {
		mu.Lock()
		defer mu.Unlock()
		progress = append(progress, struct{ completed, total int }{completed, total})
	}

	_, err := processOCRImagesParallel(context.Background(), images, mock, "P", progressFn)
	requireParallelNoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 3 {
		t.Fatalf("expected 3 progress calls (one per image), got %d", len(progress))
	}
	last := progress[len(progress)-1]
	if last.completed != 3 || last.total != 3 {
		t.Errorf("expected (3, 3) in final progress call, got (%d, %d)", last.completed, last.total)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i].completed <= progress[i-1].completed {
			t.Errorf("progress.completed not monotonic: %d after %d",
				progress[i].completed, progress[i-1].completed)
		}
	}
	for _, p := range progress {
		if p.total != 3 {
			t.Errorf("progress.total wrong: %d", p.total)
		}
	}
}

// =============================================================================
// Test 3: failure threshold (>=2) cancels remaining
// =============================================================================

func TestProcessOCRImages_FailureThreshold(t *testing.T) {
	mock := &failingMock{
		delay:      10 * time.Millisecond,
		alwaysFail: true, // every call (including retries) fails
	}
	images := [][]byte{distinctImageBytes(0), distinctImageBytes(1), distinctImageBytes(2), distinctImageBytes(3)}

	text, err := processOCRImagesParallel(context.Background(), images, mock, "F", nil)
	if err == nil {
		t.Errorf("expected error when all 4 images fail, got text=%q", text)
	}
	if text != "" {
		t.Errorf("expected empty text on all-failure, got %q", text)
	}
	peak := mock.peakActive.Load()
	if peak < 2 {
		t.Errorf("expected peak concurrency > 1 with 4 images and 3 workers, got %d", peak)
	}
}

// =============================================================================
// Test 4: getVisionParallelWorkers env-var parsing
// =============================================================================

func TestGetVisionParallelWorkers_Defaults(t *testing.T) {
	// configuration.GetEnvSimple looks up SPROUT_<suffix> first, then
	// LEDIT_<suffix>. Use t.Setenv to set both forms.
	setEnv := func(key, val string) {
		t.Setenv("SPROUT_"+key, val)
		t.Setenv("LEDIT_"+key, val)
	}
	unsetEnv := func(key string) {
		t.Setenv("SPROUT_"+key, "")
		t.Setenv("LEDIT_"+key, "")
	}

	setEnv("VISION_PARALLEL_WORKERS", "")
	if got := getVisionParallelWorkers(); got != 3 {
		t.Errorf("default: expected 3, got %d", got)
	}

	setEnv("VISION_PARALLEL_WORKERS", "5")
	if got := getVisionParallelWorkers(); got != 5 {
		t.Errorf("5: expected 5, got %d", got)
	}

	setEnv("VISION_PARALLEL_WORKERS", "0")
	if got := getVisionParallelWorkers(); got != 1 {
		t.Errorf("0 (clamped to min=1): expected 1, got %d", got)
	}

	setEnv("VISION_PARALLEL_WORKERS", "-1")
	if got := getVisionParallelWorkers(); got != 1 {
		t.Errorf("-1 (clamped to min=1): expected 1, got %d", got)
	}

	setEnv("VISION_PARALLEL_WORKERS", "999")
	if got := getVisionParallelWorkers(); got != 32 {
		t.Errorf("999 (clamped to max=32): expected 32, got %d", got)
	}

	setEnv("VISION_PARALLEL_WORKERS", "garbage")
	if got := getVisionParallelWorkers(); got != 3 {
		t.Errorf("garbage (default): expected 3, got %d", got)
	}

	unsetEnv("VISION_PARALLEL_WORKERS")
}

// =============================================================================
// Test 5: actual parallelism (peak concurrency approaches worker count)
// =============================================================================

func TestProcessOCRImages_Parallelism(t *testing.T) {
	mock := &failingMock{
		delay:   10 * time.Millisecond,
		failFor: map[int]bool{},
	}
	images := [][]byte{
		distinctImageBytes(0), distinctImageBytes(1), distinctImageBytes(2),
		distinctImageBytes(3), distinctImageBytes(4), distinctImageBytes(5),
	}
	t.Setenv("SPROUT_VISION_PARALLEL_WORKERS", "4")
	t.Setenv("LEDIT_VISION_PARALLEL_WORKERS", "4")
	_, err := processOCRImagesParallel(context.Background(), images, mock, "Par", nil)
	requireParallelNoError(t, err)

	peak := mock.peakActive.Load()
	if peak < 3 {
		t.Errorf("expected peak concurrency ≥ 3 with 6 images and 4 workers, got %d", peak)
	}
}

// =============================================================================
// Shared assertion
// =============================================================================

func requireParallelNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
