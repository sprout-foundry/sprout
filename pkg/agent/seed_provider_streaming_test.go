package agent

import (
	"context"
	"sync"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// streamingTestClient is a mock client that invokes the streaming callback
// with configurable chunks, simulating an LLM streaming response.
type streamingTestClient struct {
	chunks        []streamChunk
	streamingCB   api.StreamCallback
	model         string
	mu            sync.Mutex
	supportsVison bool
}

type streamChunk struct {
	content     string
	contentType string
}

func (m *streamingTestClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{
		Choices: []api.ChatChoice{{Message: api.Message{Role: "assistant", Content: "test"}}},
	}, nil
}

func (m *streamingTestClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	for _, ch := range m.chunks {
		if callback != nil {
			callback(ch.content, ch.contentType)
		}
	}
	return &api.ChatResponse{
		Choices: []api.ChatChoice{{Message: api.Message{Role: "assistant", Content: "final"}}},
		Usage:   api.ChatUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

func (m *streamingTestClient) CheckConnection() error                  { return nil }
func (m *streamingTestClient) SetDebug(bool)                           {}
func (m *streamingTestClient) SetModel(string) error                   { return nil }
func (m *streamingTestClient) GetModel() string                        { return m.model }
func (m *streamingTestClient) GetProvider() string                     { return "test-provider" }
func (m *streamingTestClient) GetModelContextLimit() (int, error)      { return 128000, nil }
func (m *streamingTestClient) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *streamingTestClient) SupportsVision() bool                    { return m.supportsVison }
func (m *streamingTestClient) GetVisionModel() string                  { return "" }
func (m *streamingTestClient) SendVisionRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *streamingTestClient) GetLastTPS() float64                     { return 0 }
func (m *streamingTestClient) GetAverageTPS() float64                  { return 0 }
func (m *streamingTestClient) GetTPSStats() map[string]float64         { return nil }
func (m *streamingTestClient) ResetTPSStats()                          {}
func (m *streamingTestClient) RegisterPastedImages(map[string][]api.ImageData) {}
func (m *streamingTestClient) ClearPastedImages()                      {}

// TestDoChatStream_PublishesStreamChunkEvents verifies that doChatStream
// routes streaming chunks through OutputRouter.RouteStreamChunk so they
// appear on the EventBus as stream_chunk events.
//
// This is the regression test for the bug where stream_chunk events were
// never published for WebUI agents because the callback called the raw
// streamingCallback (a no-op for WebUI) instead of RouteStreamChunk.
func TestDoChatStream_PublishesStreamChunkEvents(t *testing.T) {
	eventBus := events.NewEventBus()
	subscriberID := "test-subscriber"
	eventCh := eventBus.Subscribe(subscriberID)
	defer eventBus.Unsubscribe(subscriberID)

	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	agent := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	agent.initSubManagers()
	agent.SetEventBus(eventBus)
	agent.EnableStreaming(func(string) {}) // Simulate webui no-op callback

	mockClient := &streamingTestClient{
		model: "test-model",
		chunks: []streamChunk{
			{"Let me check", "assistant_text"},
			{" the file", "assistant_text"},
			{"Thinking about this", "reasoning"},
		},
	}

	provider, err := NewSproutProvider(agent, mockClient)
	if err != nil {
		t.Fatalf("failed to create sprout provider: %v", err)
	}

	sp := provider.(*sproutProvider)

	// Call doChatStream and verify it succeeds.
	// Pass a minimal ChatRequest (seed core type) to avoid nil dereference
	// in attachPastedImages.
	resp, err := sp.doChatStream(context.Background(), &core.ChatRequest{})
	if err != nil {
		t.Fatalf("doChatStream failed: %v", err)
	}
	if resp == nil {
		t.Fatal("doChatStream returned nil response")
	}

	// Collect all events published to the EventBus
	var streamChunks []events.UIEvent
	var otherEvents []events.UIEvent
	for {
		select {
		case ev := <-eventCh:
			if ev.Type == events.EventTypeStreamChunk {
				streamChunks = append(streamChunks, ev)
			} else {
				otherEvents = append(otherEvents, ev)
			}
		default:
			goto done
		}
	}

done:
	if len(streamChunks) == 0 {
		t.Error("expected stream_chunk events to be published to EventBus, got none")
	}

	// Verify we got the right chunks
	if len(streamChunks) != 3 {
		t.Errorf("expected 3 stream_chunk events (2 assistant_text + 1 reasoning), got %d", len(streamChunks))
	}

	// Verify the content of the first chunk
	if len(streamChunks) > 0 {
		data, ok := streamChunks[0].Data.(map[string]interface{})
		if !ok {
			t.Fatal("expected stream_chunk event data to be a map")
		}
		chunk, _ := data["chunk"].(string)
		if chunk != "Let me check" {
			t.Errorf("expected first chunk to be 'Let me check', got '%s'", chunk)
		}
	}

	// Verify streaming buffer was populated
	buffered := agent.output.GetStreamingBuffer().String()
	if buffered != "Let me check the file" {
		t.Errorf("expected streaming buffer to contain 'Let me check the file', got '%s'", buffered)
	}

	// Verify reasoning buffer was populated
	reasoningBuffered := agent.output.GetReasoningBuffer().String()
	if reasoningBuffered != "Thinking about this" {
		t.Errorf("expected reasoning buffer to contain 'Thinking about this', got '%s'", reasoningBuffered)
	}
}
