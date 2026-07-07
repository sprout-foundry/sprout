//go:build !js

package tools

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	stderrors "errors"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/errors"
)

// countingMock counts SendVisionRequest calls and supports custom response functions.
type countingMock struct {
	callCount    atomic.Int64
	responseFunc func(nImages int) (*api.ChatResponse, error)
	fallbackFunc func(imageIdx int) (*api.ChatResponse, error)
}

func (m *countingMock) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	n := m.callCount.Add(1)
	nImages := 0
	if len(messages) > 0 {
		nImages = len(messages[0].Images)
	}
	if m.responseFunc != nil && nImages > 1 {
		return m.responseFunc(nImages)
	}
	if m.fallbackFunc != nil && nImages == 1 {
		return m.fallbackFunc(int(n))
	}
	if nImages > 1 {
		var sb strings.Builder
		for i := 1; i <= nImages; i++ {
			sb.WriteString(fmt.Sprintf("===IMAGE_%d_START===Analysis of image %d===IMAGE_%d_END===", i, i, i))
		}
		return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Content: sb.String()}}}, Usage: api.ChatUsage{TotalTokens: 100, PromptTokens: 50, CompletionTokens: 50}}, nil
	}
	return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Content: "single image analysis"}}}, Usage: api.ChatUsage{TotalTokens: 50, PromptTokens: 25, CompletionTokens: 25}}, nil
}

func (m *countingMock) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *countingMock) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *countingMock) CheckConnection() error                              { return nil }
func (m *countingMock) SetDebug(bool)                                       {}
func (m *countingMock) SetModel(string) error                               { return nil }
func (m *countingMock) GetModel() string                                    { return "mock" }
func (m *countingMock) GetProvider() string                                 { return "mock" }
func (m *countingMock) GetModelContextLimit() (int, error)                  { return 128000, nil }
func (m *countingMock) ListModels(context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (m *countingMock) SupportsVision() bool                                { return true }
func (m *countingMock) SupportsConversationalVision() bool                  { return true }
func (m *countingMock) GetVisionModel() string                              { return "mock-vision" }
func (m *countingMock) GetLastTPS() float64                                 { return 0 }
func (m *countingMock) GetAverageTPS() float64                              { return 0 }
func (m *countingMock) GetTPSStats() map[string]float64                     { return nil }
func (m *countingMock) ResetTPSStats()                                      {}

// VisionCapabilities returns the safe defaults — countingMock is a
// stand-in for batch-vision tests; the actual capability values are not
// the focus of these tests, but the method is required by
// api.ClientInterface after SP-103-D3 / AUDIT-GAP-2.
func (m *countingMock) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}

func makeTestImages(n int) [][]byte {
	images := make([][]byte, n)
	for i := range images {
		images[i] = []byte(fmt.Sprintf("test-image-data-%d", i))
	}
	return images
}

func setupBatchTest(t *testing.T) func(t *testing.T) {
	t.Helper()
	resetVisionCache()
	return func(t *testing.T) { resetVisionCache() }
}

func TestAnalyzeImagesBatched_SingleProviderCall(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	req := BatchVisionRequest{Images: makeTestImages(3), Prompts: []string{"Analyze this"}}

	result, err := AnalyzeImagesBatched(context.Background(), client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.callCount.Load(); got != 1 {
		t.Errorf("expected 1 provider call, got %d", got)
	}
	if len(result.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.Results))
	}
	for i, r := range result.Results {
		if !strings.Contains(r.Description, fmt.Sprintf("image %d", i+1)) {
			t.Errorf("result[%d] missing expected content: %s", i, r.Description)
		}
	}
}

func TestAnalyzeImagesBatched_CacheHit(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	req := BatchVisionRequest{Images: makeTestImages(2), Prompts: []string{"Analyze"}}

	_, err := AnalyzeImagesBatched(context.Background(), client, req)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if got := client.callCount.Load(); got != 1 {
		t.Errorf("expected 1 call after first batch, got %d", got)
	}

	_, err = AnalyzeImagesBatched(context.Background(), client, req)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if got := client.callCount.Load(); got != 1 {
		t.Errorf("expected still 1 call after cache hit, got %d", got)
	}
}

func TestAnalyzeImagesBatched_PartialFailureFallback(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	client.responseFunc = func(nImages int) (*api.ChatResponse, error) {
		var sb strings.Builder
		sb.WriteString("===IMAGE_1_START===Analysis of image 1===IMAGE_1_END===")
		sb.WriteString("===IMAGE_3_START===Analysis of image 3===IMAGE_3_END===")
		return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Content: sb.String()}}}, Usage: api.ChatUsage{TotalTokens: 100, PromptTokens: 50, CompletionTokens: 50}}, nil
	}

	images := makeTestImages(3)
	req := BatchVisionRequest{Images: images, Prompts: []string{"Analyze"}}

	result, err := AnalyzeImagesBatched(context.Background(), client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.Results))
	}
	if !strings.Contains(result.Results[0].Description, "image 1") {
		t.Errorf("result[0] missing batch content: %s", result.Results[0].Description)
	}
	if result.Results[1].Description == "" {
		t.Error("result[1] should have fallback content, got empty")
	}
	if !strings.Contains(result.Results[2].Description, "image 3") {
		t.Errorf("result[2] missing batch content: %s", result.Results[2].Description)
	}
	if got := client.callCount.Load(); got != 2 {
		t.Errorf("expected 2 calls (1 batch + 1 fallback), got %d", got)
	}
}

func TestAnalyzeImagesBatched_Empty(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	req := BatchVisionRequest{Images: [][]byte{}, Prompts: nil}

	result, err := AnalyzeImagesBatched(context.Background(), client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(result.Results))
	}
	if got := client.callCount.Load(); got != 0 {
		t.Errorf("expected 0 provider calls, got %d", got)
	}
}

func TestAnalyzeImagesBatched_NilClient(t *testing.T) {
	defer setupBatchTest(t)(t)

	req := BatchVisionRequest{Images: makeTestImages(1), Prompts: []string{"Analyze"}}
	_, err := AnalyzeImagesBatched(context.Background(), nil, req)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	var typedErr *errors.TypedError
	if !stderrors.As(err, &typedErr) {
		t.Errorf("expected TypedError, got %T", err)
	} else if typedErr.Code != errors.CodeValidation {
		t.Errorf("expected code %s, got %s", errors.CodeValidation, typedErr.Code)
	}
}

func TestAnalyzeImagesBatched_OrderDependent(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	images := makeTestImages(2)
	img0, img1 := images[0], images[1]

	req1 := BatchVisionRequest{Images: [][]byte{img0, img1}, Prompts: []string{"Analyze"}}
	_, err := AnalyzeImagesBatched(context.Background(), client, req1)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if got := client.callCount.Load(); got != 1 {
		t.Errorf("expected 1 call after first batch, got %d", got)
	}

	req2 := BatchVisionRequest{Images: [][]byte{img1, img0}, Prompts: []string{"Analyze"}}
	_, err = AnalyzeImagesBatched(context.Background(), client, req2)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if got := client.callCount.Load(); got != 2 {
		t.Errorf("expected 2 calls (different order = cache miss), got %d", got)
	}

	_, err = AnalyzeImagesBatched(context.Background(), client, req1)
	if err != nil {
		t.Fatalf("third call error: %v", err)
	}
	if got := client.callCount.Load(); got != 2 {
		t.Errorf("expected still 2 calls (same order = cache hit), got %d", got)
	}
}

func TestAnalyzeImagesBatched_EmptyImage(t *testing.T) {
	defer setupBatchTest(t)(t)

	client := &countingMock{}
	req := BatchVisionRequest{Images: [][]byte{[]byte{}, []byte("data")}, Prompts: []string{"Analyze"}}
	_, err := AnalyzeImagesBatched(context.Background(), client, req)
	if err == nil {
		t.Fatal("expected error for empty image data")
	}
	var typedErr *errors.TypedError
	if !stderrors.As(err, &typedErr) {
		t.Errorf("expected TypedError, got %T", err)
	} else if typedErr.Code != errors.CodeValidation {
		t.Errorf("expected code %s, got %s", errors.CodeValidation, typedErr.Code)
	}
}
