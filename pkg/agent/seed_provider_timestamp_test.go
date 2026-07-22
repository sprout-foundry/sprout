package agent

import (
	"context"
	"reflect"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestStampTurnTimestamp(t *testing.T) {
	at := time.Date(2026, time.July, 22, 12, 37, 28, 0, time.FixedZone("CDT", -5*60*60))

	t.Run("zero timestamp returns input unchanged", func(t *testing.T) {
		messages := []core.Message{{Role: "user", Content: "hello"}}
		original := append([]core.Message(nil), messages...)
		sp := &sproutProvider{agent: &Agent{}}

		got := sp.stampTurnTimestamp(messages)

		if &got[0] != &messages[0] {
			t.Fatal("expected unchanged input slice")
		}
		if !reflect.DeepEqual(got, original) {
			t.Fatalf("messages changed with zero timestamp: got %#v, want %#v", got, original)
		}
	})

	t.Run("no user message returns input unchanged", func(t *testing.T) {
		messages := []core.Message{
			{Role: "system", Content: "instructions"},
			{Role: "assistant", Content: "response"},
			{Role: "tool", Content: "result"},
		}
		original := append([]core.Message(nil), messages...)
		sp := &sproutProvider{agent: &Agent{turnTimestamp: at}}

		got := sp.stampTurnTimestamp(messages)

		if &got[0] != &messages[0] {
			t.Fatal("expected unchanged input slice")
		}
		if !reflect.DeepEqual(got, original) {
			t.Fatalf("messages without a user changed: got %#v, want %#v", got, original)
		}
	})

	t.Run("already stamped latest user returns all messages unchanged", func(t *testing.T) {
		messages := []core.Message{
			{Role: "user", Content: "historical"},
			{Role: "assistant", Content: "prior response"},
			{Role: "user", Content: "<current-time>old</current-time>\n\ncurrent"},
			{Role: "assistant", Content: "trailing response"},
		}
		original := append([]core.Message(nil), messages...)
		sp := &sproutProvider{agent: &Agent{turnTimestamp: at}}

		got := sp.stampTurnTimestamp(messages)

		if &got[0] != &messages[0] {
			t.Fatal("expected unchanged input slice")
		}
		if !reflect.DeepEqual(got, original) {
			t.Fatalf("already-stamped messages changed: got %#v, want %#v", got, original)
		}
	})

	t.Run("latest user message is copied and stamped deterministically", func(t *testing.T) {
		messages := []core.Message{
			{Role: "user", Content: "historical\r\nrequest"},
			{Role: "assistant", Content: "prior response"},
			{Role: "user", Content: "current"},
			{Role: "assistant", Content: "trailing response"},
		}
		original := append([]core.Message(nil), messages...)
		sp := &sproutProvider{agent: &Agent{turnTimestamp: at}}

		got := sp.stampTurnTimestamp(messages)

		if &got[0] == &messages[0] {
			t.Fatal("expected a copied slice")
		}
		wantCurrent := "<current-time>2026-07-22T12:37:28-05:00 (Local: 2026-07-22 12:37:28, CDT)</current-time>\n\ncurrent"
		if got[2].Content != wantCurrent {
			t.Fatalf("latest user content = %q, want %q", got[2].Content, wantCurrent)
		}
		for _, i := range []int{0, 1, 3} {
			if !reflect.DeepEqual(got[i], original[i]) {
				t.Fatalf("non-current message %d changed: got %#v, want %#v", i, got[i], original[i])
			}
		}
		if !reflect.DeepEqual(messages, original) {
			t.Fatalf("caller slice mutated: got %#v, want %#v", messages, original)
		}

		again := sp.stampTurnTimestamp(got)
		if &again[0] != &got[0] {
			t.Fatal("expected an already stamped latest message to be returned unchanged")
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("timestamping was not idempotent: got %#v, want %#v", again, got)
		}
	})
}

// timestampCaptureClient is the external provider boundary for these tests. It
// records the real message slice sproutProvider sends while returning a small,
// successful response so all adapter logic runs normally.
type timestampCaptureClient struct {
	api.ClientInterface
	messages []api.Message
}

func (c *timestampCaptureClient) capture(messages []api.Message) *api.ChatResponse {
	c.messages = append([]api.Message(nil), messages...)
	return &api.ChatResponse{
		Choices: []api.ChatChoice{{Message: api.Message{Role: "assistant", Content: "ok"}}},
	}
}

func (c *timestampCaptureClient) SendChatRequest(_ context.Context, messages []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	return c.capture(messages), nil
}

func (c *timestampCaptureClient) SendChatRequestStream(_ context.Context, messages []api.Message, _ []api.Tool, _ string, _ bool, _ api.StreamCallback) (*api.ChatResponse, error) {
	return c.capture(messages), nil
}

type timestampStreamHandler struct {
	done bool
	err  error
}

func (*timestampStreamHandler) OnContent(string)   {}
func (*timestampStreamHandler) OnReasoning(string) {}
func (h *timestampStreamHandler) OnDone(*core.ChatResponse) {
	h.done = true
}
func (h *timestampStreamHandler) OnError(err error) {
	h.err = err
}

func TestSproutProviderStampsTimestampAtEveryProviderBoundary(t *testing.T) {
	at := time.Date(2026, time.July, 22, 12, 37, 28, 0, time.FixedZone("CDT", -5*60*60))
	wantCurrent := "<current-time>2026-07-22T12:37:28-05:00 (Local: 2026-07-22 12:37:28, CDT)</current-time>\n\ncurrent"

	for _, tc := range []struct {
		name string
		call func(t *testing.T, agent *Agent, sp *sproutProvider, req *core.ChatRequest)
	}{
		{
			name: "non-streaming Chat",
			call: func(t *testing.T, _ *Agent, sp *sproutProvider, req *core.ChatRequest) {
				t.Helper()
				if _, err := sp.Chat(context.Background(), req); err != nil {
					t.Fatalf("Chat: %v", err)
				}
			},
		},
		{
			name: "streaming Chat",
			call: func(t *testing.T, agent *Agent, sp *sproutProvider, req *core.ChatRequest) {
				t.Helper()
				agent.EnableStreaming(func(string) {})
				if _, err := sp.Chat(context.Background(), req); err != nil {
					t.Fatalf("Chat: %v", err)
				}
			},
		},
		{
			name: "ChatStream",
			call: func(t *testing.T, _ *Agent, sp *sproutProvider, req *core.ChatRequest) {
				t.Helper()
				handler := &timestampStreamHandler{}
				if err := sp.ChatStream(context.Background(), req, handler); err != nil {
					t.Fatalf("ChatStream: %v", err)
				}
				if handler.err != nil {
					t.Fatalf("stream handler error: %v", handler.err)
				}
				if !handler.done {
					t.Fatal("stream handler did not receive completion")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &timestampCaptureClient{}
			agent := &Agent{turnTimestamp: at}
			agent.initSubManagers()
			provider, err := NewSproutProvider(agent, client)
			if err != nil {
				t.Fatalf("NewSproutProvider: %v", err)
			}
			sp := provider.(*sproutProvider)
			messages := []core.Message{
				{Role: "user", Content: "historical"},
				{Role: "assistant", Content: "prior response"},
				{Role: "user", Content: "current"},
				{Role: "assistant", Content: "trailing response"},
			}
			original := append([]core.Message(nil), messages...)
			req := &core.ChatRequest{Messages: messages}

			tc.call(t, agent, sp, req)

			if len(client.messages) != len(messages) {
				t.Fatalf("provider received %d messages, want %d", len(client.messages), len(messages))
			}
			if client.messages[0].Content != "historical" {
				t.Fatalf("historical user content changed at provider boundary: %q", client.messages[0].Content)
			}
			if client.messages[2].Content != wantCurrent {
				t.Fatalf("latest user content at provider boundary = %q, want %q", client.messages[2].Content, wantCurrent)
			}
			if client.messages[3].Content != "trailing response" {
				t.Fatalf("trailing assistant content changed: %q", client.messages[3].Content)
			}
			if !reflect.DeepEqual(req.Messages, original) {
				t.Fatalf("provider boundary mutated request messages: got %#v, want %#v", req.Messages, original)
			}
		})
	}
}
