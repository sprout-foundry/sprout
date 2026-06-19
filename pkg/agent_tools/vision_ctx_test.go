package tools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// =============================================================================
// SP-034-1c — vision/PDF ctx-threading tests
//
// These tests verify the core invariant of the refactor: the ctx parameter
// threaded through ProcessImagesInText / AnalyzeImage / processPDFWithVisionModel
// / processOCRImages actually reaches the leaf SendVisionRequest call, so a
// cancelled context aborts in-flight vision API work instead of hanging on a
// real network request.
//
// They use a mock vision client (ctxVisionMockClient) — no network, no API keys.
// =============================================================================

// ctxVisionMockClient implements api.ClientInterface for ctx-propagation tests.
// All methods besides SendVisionRequest are no-ops that return zero values.
type ctxVisionMockClient struct {
	mu                sync.Mutex
	sendVisionCalled  bool
	// behavior controls how SendVisionRequest responds:
	//   "cancel-fast"  → check ctx.Err() and return immediately (no block)
	//   "block-until-cancel" → select on ctx.Done() (proves ctx is threaded)
	sendVisionBehavior string
}

func (m *ctxVisionMockClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	m.mu.Lock()
	m.sendVisionCalled = true
	behavior := m.sendVisionBehavior
	m.mu.Unlock()

	switch behavior {
	case "cancel-fast":
		// Check ctx immediately and return its error if cancelled.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Not cancelled — return a minimal valid response.
		return &api.ChatResponse{
			Choices: []api.Choice{{Message: api.Message{Content: "mock analysis"}}},
		}, nil

	case "block-until-cancel":
		// Block until ctx is cancelled. If ctx is NEVER cancelled, this would
		// hang forever — proving the refactor works means the caller's ctx
		// cancellation actually unblocks us.
		<-ctx.Done()
		return nil, ctx.Err()

	default:
		return &api.ChatResponse{
			Choices: []api.Choice{{Message: api.Message{Content: "mock analysis"}}},
		}, nil
	}
}

// --- remaining ClientInterface methods (no-ops) ---

func (m *ctxVisionMockClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *ctxVisionMockClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *ctxVisionMockClient) CheckConnection() error            { return nil }
func (m *ctxVisionMockClient) SetDebug(debug bool)               {}
func (m *ctxVisionMockClient) SetModel(model string) error       { return nil }
func (m *ctxVisionMockClient) GetModel() string                  { return "mock-model" }
func (m *ctxVisionMockClient) GetProvider() string               { return "mock" }
func (m *ctxVisionMockClient) GetModelContextLimit() (int, error) { return 128000, nil }
func (m *ctxVisionMockClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *ctxVisionMockClient) SupportsVision() bool  { return true }
func (m *ctxVisionMockClient) GetVisionModel() string { return "mock-vision-model" }
func (m *ctxVisionMockClient) GetLastTPS() float64    { return 0 }
func (m *ctxVisionMockClient) GetAverageTPS() float64 { return 0 }
func (m *ctxVisionMockClient) GetTPSStats() map[string]float64 { return nil }
func (m *ctxVisionMockClient) ResetTPSStats()         {}

// --- helpers ---

// writeTempPNG creates a minimal valid PNG file and returns its path.
func writeTempPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write PNG: %v", err)
	}
	return path
}

// writeTempPDF creates a minimal file that passes the looksLikePDF() check.
func writeTempPDF(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")
	// %PDF-1.4 header + minimal body. The looksLikePDF check only verifies
	// the first 5 bytes; the mock client short-circuits before real PDF parsing.
	data := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write PDF: %v", err)
	}
	return path
}

// =============================================================================
// Test 1: VisionProcessor.AnalyzeImage — pre-cancelled ctx
// =============================================================================

func TestAnalyzeImage_CancelledContext(t *testing.T) {
	mock := &ctxVisionMockClient{sendVisionBehavior: "cancel-fast"}
	vp := &VisionProcessor{visionClient: mock}

	imgPath := writeTempPNG(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	analysis, err := vp.AnalyzeImage(ctx, imgPath)

	// The mock short-circuits on the cancelled ctx, so SendVisionRequest
	// returns ctx.Err() and AnalyzeImage wraps it. The key assertion: an
	// error IS returned (pre-refactor, ctx would be context.Background() and
	// the mock would return a successful response).
	requireError(t, err, "AnalyzeImage with pre-cancelled ctx should return an error")

	// SendVisionRequest must actually have been called — otherwise the test
	// proves nothing (the error could come from GetImageData instead).
	if !mock.sendVisionCalled {
		t.Fatal("expected SendVisionRequest to be called — ctx must reach the leaf site")
	}

	// Analysis should be zero-value since the call failed.
	_ = analysis
}

// =============================================================================
// Test 2: VisionProcessor.AnalyzeImage — ctx honored DURING a call
// This is the strongest test: it proves ctx is not just received and ignored,
// but actually threaded into SendVisionRequest so cancelling unblocks the call.
// =============================================================================

func TestAnalyzeImage_RespectsContextDuringCall(t *testing.T) {
	mock := &ctxVisionMockClient{sendVisionBehavior: "block-until-cancel"}
	vp := &VisionProcessor{visionClient: mock}

	imgPath := writeTempPNG(t)

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		err error
	}
	done := make(chan result, 1)

	go func() {
		_, err := vp.AnalyzeImage(ctx, imgPath)
		done <- result{err: err}
	}()

	// Give the call time to reach SendVisionRequest (GetImageData + prompt
	// building run synchronously first), then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case res := <-done:
		// The call returned — it did NOT hang. This is the core invariant:
		// if ctx were ignored (context.Background()), the mock would block
		// forever and this select would time out.
		requireError(t, res.err, "AnalyzeImage should return an error after ctx cancellation")
	case <-time.After(1 * time.Second):
		t.Fatal("AnalyzeImage did not return within 1s of ctx cancellation — ctx is not threaded to SendVisionRequest")
	}

	if !mock.sendVisionCalled {
		t.Fatal("expected SendVisionRequest to be called — ctx must reach the leaf site")
	}
}

// =============================================================================
// Test 3: processPDFWithVisionModel — pre-cancelled ctx
// processPDFWithVisionModel is unexported but same-package, so we call directly.
// It reads the PDF file, checks looksLikePDF, then calls SendVisionRequest.
// =============================================================================

func TestProcessPDFWithVisionModel_CancelledContext(t *testing.T) {
	mock := &ctxVisionMockClient{sendVisionBehavior: "cancel-fast"}

	pdfPath := writeTempPDF(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	text, err := processPDFWithVisionModel(ctx, pdfPath, mock)

	// Pre-refactor, ctx would be context.Background() and the mock would
	// return a successful (non-empty) response. With the refactor, the
	// cancelled ctx makes the mock return ctx.Err().
	requireError(t, err, "processPDFWithVisionModel with pre-cancelled ctx should return an error")
	if text != "" {
		t.Errorf("expected empty text on cancelled ctx, got %q", text)
	}
	if !mock.sendVisionCalled {
		t.Fatal("expected SendVisionRequest to be called — ctx must reach the leaf site")
	}
}

// =============================================================================
// Test 4: processOCRImages — pre-cancelled ctx
// processOCRImages is unexported but same-package, so we call directly.
// It optimizes each image then calls SendVisionRequest per image.
// =============================================================================

func TestProcessOCRImages_CancelledContext(t *testing.T) {
	mock := &ctxVisionMockClient{sendVisionBehavior: "cancel-fast"}

	// One minimal PNG image (raw bytes). processOCRImages runs
	// OptimizeImageData on each before SendVisionRequest.
	img := writeTempPNG(t)
	data, err := os.ReadFile(img)
	if err != nil {
		t.Fatalf("read temp PNG: %v", err)
	}
	images := [][]byte{data}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	text, err := processOCRImages(ctx, images, mock, "Test")

	// processOCRImages tolerates failures (it increments a failure counter
	// and only returns an error after 2+ failures). With a single image and
	// a cancelled ctx, the OCR fails and allText stays empty, yielding an
	// "OCR failed for all extracted tests" error. Either an error OR empty
	// text satisfies the invariant: the cancelled ctx must prevent a
	// successful (non-empty) response.
	if err == nil && text != "" {
		t.Errorf("expected error or empty text with pre-cancelled ctx, but got non-empty text %q", text)
	}
	if text != "" {
		t.Errorf("expected empty text on cancelled ctx, got %q", text)
	}
	if !mock.sendVisionCalled {
		t.Fatal("expected SendVisionRequest to be called — ctx must reach the leaf site")
	}
}

// =============================================================================
// requireError — shared assertion
// =============================================================================

func requireError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatal(msg)
	}
}

// Ensure the errors package is referenced (used implicitly by context.Canceled
// comparisons in richer assertions if added later).
var _ = errors.Is
