package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

type failingClient struct {
	*factory.TestClient
	err error
}

func newFailingClient(err error) *failingClient {
	return &failingClient{TestClient: &factory.TestClient{}, err: err}
}

func (c *failingClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return nil, c.err
}

func (c *failingClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, c.err
}

func TestProcessQueryFlushesOnAPIFailure(t *testing.T) {
	failureErr := fmt.Errorf("synthetic API failure")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:             newFailingClient(failureErr),
		systemPrompt:       "test system prompt",
		maxIterations:      1,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       ctx,
		interruptCancel:    cancel,
		outputMutex:        &sync.Mutex{},
	}

	var flushed bool
	agent.SetFlushCallback(func() { flushed = true })

	done := make(chan struct{})
	var (
		response string
		err      error
	)

	go func() {
		response, err = agent.ProcessQuery("do something")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ProcessQuery timed out waiting for API failure handling")
	}

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if response == "" {
		t.Fatal("expected interactive error response to be returned")
	}

	if !flushed {
		t.Fatal("expected flush callback to run before returning from API failure")
	}
}

func TestProcessQueryStreamingErrorReturns(t *testing.T) {
	failureErr := fmt.Errorf("synthetic streaming failure")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:             newFailingClient(failureErr),
		systemPrompt:       "test system prompt",
		maxIterations:      1,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       ctx,
		interruptCancel:    cancel,
		outputMutex:        &sync.Mutex{},
	}

	// Enable streaming to exercise streaming error path
	agent.EnableStreaming(func(string) {})

	done := make(chan struct{})
	var err error

	go func() {
		_, err = agent.ProcessQuery("streaming task")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ProcessQuery timed out while handling streaming API failure")
	}

	if err != nil {
		t.Fatalf("expected no error for interactive handling, got %v", err)
	}
}
