package tools

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// =============================================================================
// SP-103-A7 — race-detector test fixtures
//
// Dedicated concurrency tests that prove the vision subsystem is race-free
// under -race.  These are NOT about functional correctness (covered by
// existing test files) — they are about data-race safety under concurrent
// access to shared state (VisionLRUCache, visionLastUsageMirror,
// processOCRImagesParallel worker pool, preflightRemoteSize).
//
// Run with: go test -race ./pkg/agent_tools/ -run TestVisionConcurrent
// =============================================================================

// ---------------------------------------------------------------------------
// concurrentMock — mock ClientInterface that tracks peak concurrency and
// returns a fixed successful response.
// ---------------------------------------------------------------------------

type concurrentMock struct {
	mu         sync.Mutex
	active     atomic.Int32
	peakActive atomic.Int32
	delay      time.Duration
}

func (m *concurrentMock) SendVisionRequest(ctx context.Context, messages []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	active := m.active.Add(1)
	defer m.active.Add(-1)
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
	content := "concurrent analysis result"
	return &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Content: content}}},
		Usage:   api.ChatUsage{TotalTokens: 100, PromptTokens: 50, CompletionTokens: 50},
	}, nil
}

func (m *concurrentMock) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *concurrentMock) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *concurrentMock) CheckConnection() error             { return nil }
func (m *concurrentMock) SetDebug(bool)                      {}
func (m *concurrentMock) SetModel(string) error              { return nil }
func (m *concurrentMock) GetModel() string                   { return "mock-concurrent" }
func (m *concurrentMock) GetProvider() string                { return "mock" }
func (m *concurrentMock) GetModelContextLimit() (int, error) { return 128000, nil }
func (m *concurrentMock) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *concurrentMock) SupportsVision() bool { return true }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (m *concurrentMock) SupportsConversationalVision() bool {
	return false
}
func (m *concurrentMock) GetVisionModel() string          { return "mock-vision" }
func (m *concurrentMock) GetLastTPS() float64             { return 0 }
func (m *concurrentMock) GetAverageTPS() float64          { return 0 }
func (m *concurrentMock) GetTPSStats() map[string]float64 { return nil }
func (m *concurrentMock) ResetTPSStats()                  {}

// VisionCapabilities returns the safe defaults — concurrentMock focuses
// on race-safety, not capability tuning. The method exists to satisfy
// api.ClientInterface after SP-103-D3 / AUDIT-GAP-2.
func (m *concurrentMock) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeDistinctPNG(t *testing.T, idx int) []byte {
	t.Helper()
	size := 8 + idx
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r := byte((idx*31 + x*7 + y*13 + 0xA7) & 0xff)
			img.Set(x, y, color.RGBA{r, r, r, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG for idx %d: %v", idx, err)
	}
	return buf.Bytes()
}

func writeDistinctPNG(t *testing.T, idx int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "img_"+intToStr(idx)+".png")
	data := makeDistinctPNG(t, idx)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write PNG: %v", err)
	}
	return path
}

// =============================================================================
// Test 1: TestVisionConcurrent_AnalyzeImage
//
// 10 goroutines call VisionProcessor.AnalyzeImage concurrently against a
// mock client.  Each goroutine uses a distinct image file and its own
// VisionProcessor (vp.usage is not mutex-protected, so sharing a single
// processor across goroutines would race on that field).
//
// Asserts:
//   - all 10 calls complete without deadlock
//   - peak concurrency ≥ 2 (proves goroutines overlapped)
//   - no data races (enforced by -race flag)
// =============================================================================

func TestVisionConcurrent_AnalyzeImage(t *testing.T) {
	mock := &concurrentMock{delay: 5 * time.Millisecond}

	const goroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errCount atomic.Int32

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// Each goroutine gets its own VisionProcessor to avoid the
			// per-processor vp.usage race (the global lastUsageMirror is
			// already mutex-protected).
			vp := &VisionProcessor{visionClient: mock}
			imgPath := writeDistinctPNG(t, idx)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := vp.AnalyzeImage(ctx, imgPath)
			if err != nil {
				errCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if got := successCount.Load(); got != int32(goroutines) {
		t.Errorf("expected %d successes, got %d (failures: %d)", goroutines, got, errCount.Load())
	}

	peak := mock.peakActive.Load()
	if peak < 2 {
		t.Errorf("expected peak concurrency ≥ 2, got %d", peak)
	}
}

// =============================================================================
// Test 2: TestVisionConcurrent_ParallelOCR6Pages
//
// Drive 6 images through processOCRImagesParallel using a flaky mock that
// fails the first 2 calls globally.  The retry wrapper in runOCROne retries
// those calls, and since the mock only fails calls 1-2, the retries succeed.
//
// Asserts:
//   - all 6 pages produce results (text is non-empty)
//   - no data races under -race
// =============================================================================

func TestVisionConcurrent_ParallelOCR6Pages(t *testing.T) {
	mock := &failingMock{
		delay:   5 * time.Millisecond,
		failFor: map[int]bool{1: true, 2: true},
	}

	images := [][]byte{
		makeDistinctPNG(t, 0),
		makeDistinctPNG(t, 1),
		makeDistinctPNG(t, 2),
		makeDistinctPNG(t, 3),
		makeDistinctPNG(t, 4),
		makeDistinctPNG(t, 5),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	text, err := processOCRImagesParallel(ctx, images, mock, "Page", nil)
	if err != nil {
		t.Fatalf("processOCRImagesParallel failed: %v", err)
	}
	if text == "" {
		t.Fatal("expected non-empty text from 6 pages, got empty string")
	}

	// Verify at least some page separators exist (pages that succeeded on
	// first try or after retry will appear in order).
	sepCount := 0
	for i := 2; i <= 6; i++ {
		marker := "--- Page " + intToStr(i) + " ---"
		if containsMarker(text, marker) {
			sepCount++
		}
	}
	if sepCount < 2 {
		t.Errorf("expected at least 2 page separators, got %d", sepCount)
	}
}

// containsMarker is a simple substring check.
func containsMarker(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// Test 3: TestVisionConcurrent_CacheThrash
//
// 100 goroutines thrash a VisionLRUCache with mixed Get/Put operations,
// while a separate goroutine continuously reads Stats(), GetLastVisionUsage(),
// and GetVisionCacheStats().  This exercises:
//   - VisionLRUCache mutex protection
//   - visionCacheStats atomic counters
//   - visionLastUsageMirror RWMutex
//   - GetVisionCacheStats (locks visionLRU.mu while reading atomic stats)
//
// Asserts: completion without races (enforced by -race flag).
// =============================================================================

func TestVisionConcurrent_CacheThrash(t *testing.T) {
	const cacheCap = 16
	cache := NewVisionLRUCache(cacheCap)

	ClearLastVisionUsage()

	// Separate WaitGroups: workersWg for put/get workers, readerWg for the
	// stats reader.  This avoids the deadlock of waiting on a WaitGroup
	// that includes a reader blocked on a stop flag that hasn't been set yet.
	var workersWg sync.WaitGroup
	var readerWg sync.WaitGroup
	var stopReader atomic.Bool

	// Reader goroutine: runs until stopReader is set.
	readerWg.Add(1)
	go func() {
		defer readerWg.Done()
		for !stopReader.Load() {
			_ = cache.Stats()
			_ = GetLastVisionUsage()
			_ = GetVisionCacheStats()
			time.Sleep(100 * time.Microsecond)
		}
	}()

	// 50 goroutines doing Put operations.
	putCount := 50
	workersWg.Add(putCount)
	for i := 0; i < putCount; i++ {
		go func(idx int) {
			defer workersWg.Done()
			key := "image_" + intToStr(idx)
			result := "result_for_" + intToStr(idx)
			usage := &VisionUsageInfo{
				PromptTokens:     50 + idx,
				CompletionTokens: 30 + idx,
				TotalTokens:      80 + idx,
				EstimatedCost:    0.001 * float64(idx),
			}
			for j := 0; j < 20; j++ {
				iterationKey := key + "_" + intToStr(j)
				cache.Put(iterationKey, result, usage)
				recordVisionUsage(nil, usage)
			}
		}(i)
	}

	// 50 goroutines doing Get operations.
	getCount := 50
	workersWg.Add(getCount)
	for i := 0; i < getCount; i++ {
		go func(idx int) {
			defer workersWg.Done()
			for j := 0; j < 20; j++ {
				key := "image_" + intToStr(idx%50) + "_" + intToStr(j%20)
				_, _, _ = cache.Get(key)
			}
		}(i)
	}

	// Wait for workers to finish, then stop the reader.
	workersWg.Wait()
	stopReader.Store(true)
	readerWg.Wait()

	// Verify cache is in a consistent state.
	stats := cache.Stats()
	if stats.Size > int64(cacheCap) {
		t.Errorf("cache size %d exceeds capacity %d", stats.Size, cacheCap)
	}
	if stats.Misses == 0 {
		t.Error("expected at least some cache misses during thrash")
	}
}

// =============================================================================
// Test 4: TestVisionConcurrent_PreflightUnderLoad
//
// 50 goroutines each hit a unique httptest.Server configured with a
// different response pattern.  This exercises preflightRemoteSize under
// concurrent conditions with real HTTP connections.
//
// Server configs cycle through:
//   - HEAD 200 with Content-Length OK (small file)
//   - HEAD 200 with Content-Length too large (oversized)
//   - HEAD 405 Method Not Allowed
//   - HEAD 204 No Content
//   - HEAD 200 with no Content-Length header
//
// Asserts: all goroutines complete without races or deadlocks.
// =============================================================================

func TestVisionConcurrent_PreflightUnderLoad(t *testing.T) {
	const goroutines = 50

	configs := []http.HandlerFunc{
		// HEAD 200 with small Content-Length
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
		},
		// HEAD 200 with oversized Content-Length
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10737418240") // 10GB
			w.WriteHeader(http.StatusOK)
		},
		// HEAD 405 Method Not Allowed
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
		},
		// HEAD 204 No Content
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		// HEAD 200 with no Content-Length
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	}

	servers := make([]*httptest.Server, len(configs))
	for i, handler := range configs {
		servers[i] = httptest.NewServer(handler)
		defer servers[i].Close()
	}

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var exceededCount atomic.Int32

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			serverIdx := idx % len(servers)
			url := servers[serverIdx].URL
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := preflightRemoteSize(ctx, url, 20*1024*1024)
			switch {
			case err == nil:
				successCount.Add(1)
			case IsRemoteSizeExceededError(err):
				exceededCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	total := successCount.Load() + exceededCount.Load()
	if int(total) != goroutines {
		t.Errorf("expected %d total results, got %d", goroutines, total)
	}

	if exceededCount.Load() == 0 {
		t.Error("expected at least one size-exceeded error from oversized config")
	}

	if successCount.Load() < 10 {
		t.Errorf("expected at least 10 successful preflights, got %d", successCount.Load())
	}
}
