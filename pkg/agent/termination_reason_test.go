package agent

import (
	"context"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

type scriptedClient struct {
	*factory.TestClient
	responses []*api.ChatResponse
	index     int
}

func newScriptedClient(responses ...*api.ChatResponse) *scriptedClient {
	return &scriptedClient{
		TestClient: &factory.TestClient{},
		responses:  responses,
	}
}

func (c *scriptedClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	if len(c.responses) == 0 {
		return &api.ChatResponse{}, nil
	}
	if c.index >= len(c.responses) {
		return c.responses[len(c.responses)-1], nil
	}
	resp := c.responses[c.index]
	c.index++
	return resp, nil
}

func (c *scriptedClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return c.SendChatRequest(messages, tools, reasoning)
}

func TestProcessQuerySetsMaxIterationsTerminationReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp := &api.ChatResponse{
		Choices: []api.Choice{{
			FinishReason: "",
		}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.Content = "Working through the next step."

	agent := &Agent{
		client:             newScriptedClient(resp),
		systemPrompt:       "system",
		maxIterations:      1,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       ctx,
		interruptCancel:    cancel,
		outputMutex:        &sync.Mutex{},
	}

	_, err := agent.ProcessQuery("Investigate the issue")
	if err != nil {
		t.Fatalf("ProcessQuery returned error: %v", err)
	}

	if got := agent.GetLastRunTerminationReason(); got != RunTerminationMaxIterations {
		t.Fatalf("expected termination reason %q, got %q", RunTerminationMaxIterations, got)
	}
}
