package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestIsBodyTooLargeError pins the 413 classifier: provider HTTP errors are
// formatted as "HTTP <code>..." and the match must be code-precise so a
// "HTTP 4130" (or 4131..4139) never classifies as body-too-large.
func TestIsBodyTooLargeError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("HTTP 413: body too large"), true},
		{fmt.Errorf("HTTP 413"), true},
		{fmt.Errorf("HTTP 413 (empty body, headers: x)"), true},
		{fmt.Errorf("wrapped: HTTP 413: Request Entity Too Large"), true},
		{fmt.Errorf("HTTP 4130: weird"), false},
		{fmt.Errorf("HTTP 500: internal"), false},
		{fmt.Errorf("context deadline exceeded"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isBodyTooLargeError(c.err); got != c.want {
			t.Errorf("isBodyTooLargeError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// bodyLimitClient fails its first fail413 chat calls with an HTTP 413 and
// records the image count of every request it sees. It reports
// vision-capable so attachPastedImages actually attaches (a non-vision
// client would early-return and mask the paste-clear behavior under test).
type bodyLimitClient struct {
	*MockClient
	fail413    int
	calls      int
	seenImages []int
}

func (c *bodyLimitClient) SupportsVision() bool { return true }

func (c *bodyLimitClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	c.calls++
	c.seenImages = append(c.seenImages, api.CountImages(messages))
	if c.calls <= c.fail413 {
		return nil, fmt.Errorf("HTTP 413: body too large")
	}
	return c.MockClient.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

func (c *bodyLimitClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	c.calls++
	c.seenImages = append(c.seenImages, api.CountImages(messages))
	if c.calls <= c.fail413 {
		return nil, fmt.Errorf("HTTP 413: body too large")
	}
	if callback != nil {
		callback("ok", "assistant_text")
	}
	return c.MockClient.SendChatRequestStream(ctx, messages, tools, reasoning, disableThinking, callback)
}

func imagesMessageSet(n int) []api.Message {
	messages := make([]api.Message, 0, n)
	for i := 0; i < n; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("screenshot %d", i),
			Images: []api.ImageData{
				{Base64: fmt.Sprintf("AAAA%d", i), Type: "image/png"},
			},
		})
	}
	return messages
}

// TestStreaming413ShedsImagesAndRetries runs the full cascade: 4 live
// images, two 413s. Each 413 sheds half the remaining images (most recent
// kept): 4 -> 2 -> 1. Verify the per-attempt image counts and that the
// final attempt succeeds.
func TestStreaming413ShedsImagesAndRetries(t *testing.T) {
	client := &bodyLimitClient{MockClient: &MockClient{model: "m"}, fail413: 2}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}

	resp, err := provider.(*sproutProvider).doChatWithRetryStreaming(
		context.Background(), imagesMessageSet(4), nil, "", nil, nil)
	if err != nil {
		t.Fatalf("doChatWithRetryStreaming: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}

	// Attempt 0: 4 images -> 413, shed to 2. Attempt 1: 2 images -> 413,
	// shed to 1. Attempt 2: 1 image -> success.
	want := []int{4, 2, 1}
	if len(client.seenImages) != len(want) {
		t.Fatalf("client saw %d requests (%v), want %d (%v)", len(client.seenImages), client.seenImages, len(want), want)
	}
	for i, w := range want {
		if client.seenImages[i] != w {
			t.Errorf("request %d carried %d images, want %d (sequence %v)", i, client.seenImages[i], w, client.seenImages)
		}
	}
}

// TestStreaming413ExhaustsToTextOnly pins the terminal state: when every
// image is shed (n/2 == 0) the text-only request goes out, and if the
// server still 413s a text-only body there is nothing left to shed — the
// error surfaces instead of looping.
func TestStreaming413ExhaustsToTextOnly(t *testing.T) {
	client := &bodyLimitClient{MockClient: &MockClient{model: "m"}, fail413: 99}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}

	_, err = provider.(*sproutProvider).doChatWithRetryStreaming(
		context.Background(), imagesMessageSet(4), nil, "", nil, nil)
	if err == nil {
		t.Fatal("expected the unfixable 413 to surface, got success")
	}
	if !isBodyTooLargeError(err) {
		t.Errorf("error should be the 413, got %v", err)
	}
	// 4 -> 2 -> 1 -> 0: the final attempt must be text-only.
	if got := client.seenImages[len(client.seenImages)-1]; got != 0 {
		t.Errorf("final request carried %d images, want 0 (sequence %v)", got, client.seenImages)
	}
}

// TestChat413ShedsImagesAndClearsPasted pins the Chat (non-streaming) path:
// after a 413 the retry sheds images AND clears registered pasted images —
// otherwise doChatOnce's prep pipeline re-attaches them on the next
// attempt and the shed is undone.
//
// The client must be vision-capable: attachPastedImages early-returns for
// non-vision clients, which would make the paste never attach and the test
// pass with or without clearPastedImages.
func TestChat413ShedsImagesAndClearsPasted(t *testing.T) {
	client := &bodyLimitClient{MockClient: &MockClient{model: "m"}, fail413: 2}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}
	sp := provider.(*sproutProvider)

	// Register a pasted image so the Chat path's prep pipeline would
	// re-attach it on every attempt.
	sp.RegisterPastedImages(map[string][]api.ImageData{
		"pasted.png": {{Base64: "PASTE", Type: "image/png"}},
	})

	req := &core.ChatRequest{
		Messages: append([]core.Message(nil), imagesMessageSet(4)...),
	}
	if _, err := sp.doChatWithRetry(context.Background(), req); err != nil {
		t.Fatalf("doChatWithRetry: %v", err)
	}

	// Attempt 0: 4 request images + 1 pasted = 5, live-trimmed to the
	// budget of 3 -> 413. Shed against the wire view: min(4,3)/2 = 1, and
	// the paste is cleared. Attempt 1 carries 1 image -> 413. Shed:
	// min(1,3)/2 = 0. Attempt 2 is text-only and succeeds.
	// Without clearPastedImages the sequences would read [3, 2, 1] — the
	// paste re-attaching one image per attempt.
	want := []int{3, 1, 0}
	if len(client.seenImages) != len(want) {
		t.Fatalf("client saw %d requests (%v), want %d (%v)", len(client.seenImages), client.seenImages, len(want), want)
	}
	for i, w := range want {
		if client.seenImages[i] != w {
			t.Errorf("request %d carried %d images, want %d (sequence %v)", i, client.seenImages[i], w, client.seenImages)
		}
	}
	if n := len(sp.pastedImages); n != 0 {
		t.Errorf("pasted images not cleared after 413: %d remain", n)
	}
}

func newBudgetTestAgent(t *testing.T) *Agent {
	t.Helper()
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("config manager: %v", err)
	}
	agent := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	agent.initSubManagers()
	return agent
}

// TestTrimImagesBeyondLatestKeepsMostRecent pins the body-budget trim: with
// a budget of 3 over 5 images, the two oldest are withheld and noted.
func TestTrimImagesBeyondLatestKeepsMostRecent(t *testing.T) {
	messages := imagesMessageSet(5)
	trimmed := api.TrimImagesBeyondLatest(messages, 3)

	if n := api.CountImages(trimmed); n != 3 {
		t.Fatalf("CountImages(trimmed) = %d, want 3", n)
	}
	// Most recent kept, oldest shed with a note.
	if trimmed[0].Images != nil || !strings.Contains(trimmed[0].Content, "withheld") {
		t.Errorf("oldest message not withheld: images=%v content=%q", trimmed[0].Images, trimmed[0].Content)
	}
	if trimmed[4].Images == nil {
		t.Error("newest message lost its image")
	}
	// Input must be untouched (stable wire prefix).
	if api.CountImages(messages) != 5 {
		t.Error("TrimImagesBeyondLatest mutated its input")
	}
}
