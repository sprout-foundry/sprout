package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

type captureToolsClient struct {
	tools []api.Tool
}

func (c *captureToolsClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	c.tools = append([]api.Tool(nil), tools...)
	return &api.ChatResponse{
		Choices: []api.Choice{
			{
				FinishReason: "stop",
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{
					Role:    "assistant",
					Content: "ok",
				},
			},
		},
	}, nil
}

func (c *captureToolsClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return c.SendChatRequest(messages, tools, reasoning)
}

func (c *captureToolsClient) CheckConnection() error { return nil }
func (c *captureToolsClient) SetDebug(debug bool)    {}
func (c *captureToolsClient) SetModel(model string) error {
	return nil
}
func (c *captureToolsClient) GetModel() string                     { return "test-model" }
func (c *captureToolsClient) GetProvider() string                  { return "deepinfra" }
func (c *captureToolsClient) GetModelContextLimit() (int, error)   { return 128000, nil }
func (c *captureToolsClient) ListModels() ([]api.ModelInfo, error) { return nil, nil }
func (c *captureToolsClient) SupportsVision() bool                 { return true }
func (c *captureToolsClient) GetVisionModel() string               { return "google/gemma-3-27b-it" }
func (c *captureToolsClient) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return c.SendChatRequest(messages, tools, reasoning)
}
func (c *captureToolsClient) GetLastTPS() float64             { return 0 }
func (c *captureToolsClient) GetAverageTPS() float64          { return 0 }
func (c *captureToolsClient) GetTPSStats() map[string]float64 { return map[string]float64{} }
func (c *captureToolsClient) ResetTPSStats()                  {}

func toolNames(tools []api.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Function.Name)
	}
	return names
}

func hasTool(tools []api.Tool, name string) bool {
	for _, t := range tools {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

func TestDebugToolPayload_DeepInfraOrchestratorIncludesAnalyzeUIScreenshot(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	cfg := agent.GetConfigManager().GetConfig()
	if cfg.CustomProviders == nil {
		cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
	}
	// Ensure deepinfra custom provider does not restrict tools in this scenario.
	cp := cfg.CustomProviders["deepinfra"]
	cp.ToolCalls = nil
	cfg.CustomProviders["deepinfra"] = cp

	capture := &captureToolsClient{}
	agent.client = capture
	agent.clientType = api.DeepInfraClientType
	agent.activePersona = "orchestrator"
	agent.messages = []api.Message{{Role: "user", Content: "test"}}

	handler := NewConversationHandler(agent)
	_, err = handler.sendMessage()
	if err != nil {
		t.Fatalf("sendMessage failed: %v", err)
	}

	t.Logf("tool payload count=%d names=%v", len(capture.tools), toolNames(capture.tools))
	if !hasTool(capture.tools, "analyze_ui_screenshot") {
		t.Fatalf("expected analyze_ui_screenshot in payload")
	}
}

func TestDebugToolPayload_DeepInfraCustomAllowlistExcludesAnalyzeUIScreenshot(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	cfg := agent.GetConfigManager().GetConfig()
	if cfg.CustomProviders == nil {
		cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
	}
	cfg.CustomProviders["deepinfra"] = configuration.CustomProviderConfig{
		Name:      "deepinfra",
		ToolCalls: []string{"read_file", "shell_command"},
	}

	capture := &captureToolsClient{}
	agent.client = capture
	agent.clientType = api.DeepInfraClientType
	agent.activePersona = "orchestrator"
	agent.messages = []api.Message{{Role: "user", Content: "test"}}

	handler := NewConversationHandler(agent)
	_, err = handler.sendMessage()
	if err != nil {
		t.Fatalf("sendMessage failed: %v", err)
	}

	t.Logf("tool payload count=%d names=%v", len(capture.tools), toolNames(capture.tools))
	if hasTool(capture.tools, "analyze_ui_screenshot") {
		t.Fatalf("did not expect analyze_ui_screenshot in payload")
	}
}
