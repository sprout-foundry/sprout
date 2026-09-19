//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestGetStructuredDescriptionPrompt(t *testing.T) {
	prompt := GetStructuredDescriptionPrompt()
	for _, want := range []string{"Layout", "Text regions", "Colors", "Components/UI"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("structured prompt missing %q section", want)
		}
	}
}

// TestDelegateImageDescriptions exercises the delegation rung through a
// scripted vision client: the structured prompt rides along, the
// description and provenance come back attributed.
func TestDelegateImageDescriptions(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
	}, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	// Minimal OpenAI-compatible chat server capturing the request.
	var gotPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		body := string(bodyBytes)
		if idx := strings.Index(body, "Describe this image"); idx >= 0 {
			gotPrompt = body[idx:]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"glm-5v-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"Layout: single column. Colors: #fff bg."},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"estimated_cost":0}}`))
	}))
	defer server.Close()

	// Register a fake provider config pointing at the test server? No —
	// DelegateImageDescriptions with an explicit client avoids the factory.
	client := &delegateTestClient{serverURL: server.URL}

	result, err := DelegateImageDescriptions(context.Background(), client, imgPath)
	if err != nil {
		t.Fatalf("DelegateImageDescriptions: %v", err)
	}
	if !strings.Contains(result.Description, "single column") {
		t.Errorf("description should come from the model, got %q", result.Description)
	}
	if result.Provider != "delegate-test" || result.Model != "vision-model" {
		t.Errorf("provenance = %s/%s, want delegate-test/vision-model", result.Provider, result.Model)
	}
	if !strings.Contains(gotPrompt, "Text regions") {
		t.Error("structured description prompt should be sent to the vision model")
	}
}

// delegateTestClient is a minimal vision-capable client posting to the test
// server. Implements only what VisionProcessor.AnalyzeImage touches.
type delegateTestClient struct {
	serverURL string
}

func (c *delegateTestClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return c.send(ctx, messages)
}

func (c *delegateTestClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return c.send(ctx, messages)
}

func (c *delegateTestClient) send(ctx context.Context, messages []api.Message) (*api.ChatResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, nil)
	if err != nil {
		return nil, err
	}
	// Serialize messages into the body so the handler sees the prompt.
	payload := "prompt:" + messages[len(messages)-1].Content
	req.Body = io.NopCloser(strings.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out api.ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *delegateTestClient) CheckConnection() error             { return nil }
func (c *delegateTestClient) SetDebug(bool)                      {}
func (c *delegateTestClient) SetModel(string) error              { return nil }
func (c *delegateTestClient) GetModel() string                   { return "vision-model" }
func (c *delegateTestClient) GetProvider() string                { return "delegate-test" }
func (c *delegateTestClient) GetModelContextLimit() (int, error) { return 128000, nil }
func (c *delegateTestClient) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (c *delegateTestClient) SupportsVision() bool { return true }
func (c *delegateTestClient) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}
func (c *delegateTestClient) GetVisionModel() string { return "vision-model" }
func (c *delegateTestClient) GetLastTPS() float64    { return 0 }
func (c *delegateTestClient) GetAverageTPS() float64 { return 0 }
func (c *delegateTestClient) GetTPSStats() map[string]float64 {
	return nil
}
func (c *delegateTestClient) ResetTPSStats() {}
func (c *delegateTestClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return c.send(ctx, messages)
}
