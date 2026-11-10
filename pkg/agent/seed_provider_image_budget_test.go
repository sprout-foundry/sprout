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
		context.Background(), imagesMessageSet(4), nil, "", false, nil, nil, nil, nil)
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

// TestStreaming413ExhaustsToTextOnly pins the cascade's terminal state:
// once every image is shed (n/2 == 0), the retry switches to shedding the
// text history — the request goes out text-only and, when it still 413s,
// the oldest non-system turns are dropped on each remaining attempt rather
// than failing immediately. With retries exhausted the 413 surfaces.
func TestStreaming413ExhaustsToTextOnly(t *testing.T) {
	client := &bodyLimitClient{MockClient: &MockClient{model: "m"}, fail413: 99}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}

	_, err = provider.(*sproutProvider).doChatWithRetryStreaming(
		context.Background(), imagesMessageSet(4), nil, "", false, nil, nil, nil, nil)
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

// textTrimmingClient fails every call with a 413 and records the message
// count of each request, so a test can assert the text history shrinks
// once images are gone.
type textTrimmingClient struct {
	*MockClient
	calls      int
	seenCounts []int
	seenImages []int
}

func (c *textTrimmingClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	c.calls++
	c.seenCounts = append(c.seenCounts, len(messages))
	c.seenImages = append(c.seenImages, api.CountImages(messages))
	return nil, fmt.Errorf("HTTP 413: body too large")
}

// TestChat413TrimsTextHistoryWhenImagesExhausted pins the second stage of
// the cascade: a long text-only history that keeps 413ing is shrunk by
// dropping the oldest non-system messages, keeping the system prompt and a
// recent-turn tail. Without this the retry gave up as soon as images were
// gone, so a session whose text body alone exceeded the server limit could
// never recover.
func TestChat413TrimsTextHistoryWhenImagesExhausted(t *testing.T) {
	client := &textTrimmingClient{MockClient: &MockClient{model: "m"}}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}
	sp := provider.(*sproutProvider)

	// A system prompt plus a long user/assistant history, no images.
	msgs := []core.Message{{Role: "system", Content: "system prompt"}}
	for i := 0; i < 40; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, core.Message{Role: role, Content: fmt.Sprintf("turn %d", i)})
	}
	req := &core.ChatRequest{Messages: msgs}

	_, err = sp.doChatWithRetry(context.Background(), req)
	if err == nil {
		t.Fatal("expected the unfixable 413 to surface")
	}

	// The history must have been truncated on retries: later requests carry
	// fewer messages than the first.
	if len(client.seenCounts) < 2 {
		t.Fatalf("expected multiple attempts, saw %d", len(client.seenCounts))
	}
	first, last := client.seenCounts[0], client.seenCounts[len(client.seenCounts)-1]
	if last >= first {
		t.Errorf("text history did not shrink: first=%d last=%d (counts %v)", first, last, client.seenCounts)
	}
	// The system prompt must survive every truncation.
	if req.Messages[0].Role != "system" {
		t.Errorf("system prompt dropped: first role = %q", req.Messages[0].Role)
	}
}

// TestChat413AlwaysFailingTerminates: an endpoint that answers 413 to every
// request, including the smallest one the cascade can build, must end the
// call with the 413 instead of trimming forever.
func TestChat413AlwaysFailingTerminates(t *testing.T) {
	client := &textTrimmingClient{MockClient: &MockClient{model: "m"}}
	testAgent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(testAgent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}
	sp := provider.(*sproutProvider)

	msgs := []core.Message{{Role: "system", Content: "system prompt"}}
	for i := 0; i < 40; i++ {
		msgs = append(msgs, core.Message{Role: "user", Content: fmt.Sprintf("turn %d", i)})
	}

	_, err = sp.doChatWithRetry(context.Background(), &core.ChatRequest{Messages: msgs})
	if !isBodyTooLargeError(err) {
		t.Fatalf("err = %v, want the 413 to surface", err)
	}
	if len(client.seenCounts) > maxInPlaceRetries+2 {
		t.Errorf("attempts = %d, want the in-place budget to bound them", len(client.seenCounts))
	}
}

// TestTrimMessagesForBodyBudget pins the helper directly: system messages
// are always kept, the tail is kept, the cut lands on a user boundary so a
// tool sequence is never split, and the input is not mutated.
func TestTrimMessagesForBodyBudget(t *testing.T) {
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "a0", ToolCalls: []api.ToolCall{{ID: "t0"}}},
		{Role: "tool", Content: "r0", ToolCallID: "t0"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u2"},
	}

	got := api.TrimMessagesForBodyBudget(msgs, 3)

	// System kept, tail of 3 starts at a user boundary (u1), so the kept
	// window is sys,u1,a2,u2.
	if got[0].Role != "system" {
		t.Fatalf("system message dropped: %v", got)
	}
	if got[1].Content != "u1" {
		t.Errorf("cut did not land on a user boundary: got %q, want u1 (full %v)", got[1].Content, got)
	}
	if len(got) != 4 {
		t.Errorf("len(got) = %d, want 4 (%v)", len(got), got)
	}
	// Input untouched.
	if len(msgs) != 8 {
		t.Errorf("input mutated: len = %d", len(msgs))
	}

	// Nothing to drop: an already-small history is returned unchanged.
	small := []api.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}
	if out := api.TrimMessagesForBodyBudget(small, 16); len(out) != 2 {
		t.Errorf("small history trimmed: %v", out)
	}
}

// TestChat413ShedsImagesAndClearsPasted pins the Chat (non-streaming) path:
// when the request's text body is already at its floor, a 413 sheds image
// bytes (most recent kept) and clears the registered pasted image —
// otherwise doChatOnce's prep pipeline re-attaches it on the next attempt
// and the shed is undone.
//
// The client must be vision-capable: attachPastedImages early-returns for
// non-vision clients, which would make the paste never attach and the test
// pass with or without clearPastedImages.
//
// Here the history is short (4 request messages), so the 413 cascade's text
// stage is a no-op and it falls through to the image stage. Each 413 halves
// the image bytes (most recent kept): 5 → 2 → 1, with the pasted image
// cleared on the first image shed.
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

	// Attempt 0: 4 request images + 1 pasted = 5, all under the 8MB budget.
	// 413 (call 1): text stage is a no-op (4 messages < floor), so the image
	// stage halves the 20 bytes of image mass to 10 → 2 images kept, and the
	// paste is cleared. Attempt 1 carries 2 images → 413 (call 2): halves to
	// 5 → 1 image kept. Attempt 2 carries 1 image → success (call 3).
	want := []int{5, 2, 1}
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

// TestChat413ShedsTextFirstKeepsCurrentImage pins the fix for a vision model
// in a long session: the 413 is the TEXT history's fault, not a handful of
// images, so the cascade sheds old history text first and leaves the current
// turn's attached image intact. The pre-fix behavior shed the current
// image (the user's explicit intent) to make room for the old text — exactly
// backwards.
func TestChat413ShedsTextFirstKeepsCurrentImage(t *testing.T) {
	client := &bodyLimitClient{MockClient: &MockClient{model: "m"}, fail413: 99}
	agent := newBudgetTestAgent(t)
	provider, err := NewSproutProvider(agent, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}
	sp := provider.(*sproutProvider)

	// A current-turn user message carrying the attached image, buried under a
	// long text history: the 413 is the text's fault.
	long := make([]core.Message, 0, 40)
	for i := 0; i < 38; i++ {
		long = append(long, core.Message{Role: "user", Content: "history " + strings.Repeat("x", 400)})
	}
	long = append(long, core.Message{Role: "user", Content: "here is the screenshot",
		Images: []api.ImageData{{Base64: "CURRENT", Type: "image/png"}}})

	req := &core.ChatRequest{Messages: long}
	// A long-session 413 is not fixable by shedding (the text alone keeps
	// overflowing until the tail floor), so the retry budget exhausts and the
	// 413 surfaces — the point is what the cascade did to the image before.
	if _, err := sp.doChatWithRetry(context.Background(), req); err == nil {
		t.Fatalf("expected the unfixable 413 to surface, got success")
	}

	// Every 413 attempt shrank the text tail and re-sent the request with the
	// current image still attached: the last request still carries it.
	if n := api.CountImages(req.Messages); n != 1 {
		t.Fatalf("current-turn image lost to the 413 cascade: %d images remain, want 1", n)
	}
	if api.MessagesBodyImageBytes(req.Messages) <= 0 {
		t.Errorf("current-turn image payload empty: the cascade shed the very image it should keep")
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
