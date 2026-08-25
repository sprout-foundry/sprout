package modelprobe

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestVision_Pass(t *testing.T) {
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("v1", "describe_image", map[string]string{"color": "red"})),
	}}
	o := runVision(context.Background(), c)
	if !o.passed {
		t.Fatalf("vision should pass, got reason %q", o.reason)
	}
	if o.score != 1.0 {
		t.Errorf("score = %.2f, want 1.0", o.score)
	}
	if o.stats.turns != 1 {
		t.Errorf("expected 1 turn, got %d", o.stats.turns)
	}
}

func TestVision_FailWrongColor(t *testing.T) {
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("v1", "describe_image", map[string]string{"color": "blue"})),
	}}
	o := runVision(context.Background(), c)
	if o.passed {
		t.Fatal("should fail when the model identifies the wrong color")
	}
	if !strings.Contains(o.reason, "blue") {
		t.Errorf("reason should mention the wrong color, got %q", o.reason)
	}
}

func TestVision_FailNoToolCall(t *testing.T) {
	c := &scriptedClient{turns: []api.Message{
		{Role: "assistant", Content: "The image is red."},
	}}
	o := runVision(context.Background(), c)
	if o.passed {
		t.Fatal("prose-only response must fail the vision check")
	}
	if !strings.Contains(o.reason, "describe_image") {
		t.Errorf("reason should mention missing tool call, got %q", o.reason)
	}
}

func TestVision_TransportError(t *testing.T) {
	c := &erroringClient{} // errors on the very first request
	o := runVision(context.Background(), c)
	if o.stats.err == nil {
		t.Fatal("transport error should be propagated in stats.err")
	}
	if o.passed {
		t.Error("should not pass when transport errors")
	}
}

// erroringWithMsgClient returns a fixed error message on the first request.
type erroringWithMsgClient struct {
	api.ClientInterface
	msg string
}

func (c *erroringWithMsgClient) SendChatRequest(_ context.Context, _ []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	return nil, errors.New(c.msg)
}

func TestVision_UnsupportedImageInputIsDefinitiveFail(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"deepinfra style", "HTTP 405: Model MiniMaxAI/MiniMax-M2.7 does not accept image input"},
		{"openrouter style", "HTTP 404: No endpoints found that support image input"},
		{"tool unsupported but vision implied", "HTTP 404: No endpoints found that support tool use. Try disabling \"describe_image\"."},
		{"generic 400", "HTTP 400: vision modality not supported for this model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &erroringWithMsgClient{msg: tc.msg}
			o := runVision(context.Background(), c)
			if o.stats.err != nil {
				t.Fatalf("4xx image-unsupported should NOT be a transport error, got: %v", o.stats.err)
			}
			if o.passed {
				t.Error("should be a definitive vision=false, not pass")
			}
			if !strings.Contains(o.reason, "rejected image input") {
				t.Errorf("reason should mention rejected image input, got %q", o.reason)
			}
		})
	}
}

func TestVision_5xxIsStillTransportError(t *testing.T) {
	c := &erroringWithMsgClient{msg: "HTTP 500: internal server error"}
	o := runVision(context.Background(), c)
	if o.stats.err == nil {
		t.Fatal("5xx without image keywords should remain a transport error")
	}
}

func TestVisionImage_GeneratesValidPNG(t *testing.T) {
	img, err := visionImage()
	if err != nil {
		t.Fatalf("visionImage failed: %v", err)
	}
	if img.Type != "image/png" {
		t.Errorf("type = %q, want image/png", img.Type)
	}
	if img.Base64 == "" {
		t.Fatal("base64 data should not be empty")
	}
	// Verify it decodes to valid PNG bytes
	raw, err := base64.StdEncoding.DecodeString(img.Base64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	// Check PNG magic bytes
	if len(raw) < 8 || raw[0] != 137 || raw[1] != 80 || raw[2] != 78 || raw[3] != 71 {
		t.Fatal("generated data does not start with PNG magic bytes")
	}
}

func TestRun_VisionAndGatesAndComplex(t *testing.T) {
	// Vision pass + all 5 gates pass + complex pass
	turns := []api.Message{
		turn(toolCall("v1", "describe_image", map[string]string{"color": "red"})), // vision
	}
	turns = append(turns, passingGateTurns()...) // 5 gates
	turns = append(turns,
		turn(toolCall("c1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("c2", "read_file", map[string]string{"path": "internal/users/service.go"})),
		turn(toolCall("c3", "read_file", map[string]string{"path": "internal/notify/subscriptions.go"})),
		turn(toolCall("c4", "submit_todos", map[string]string{"summary": "subscription not removed on delete", "todos": goodTodos})),
	)
	c := &scriptedClient{turns: turns}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Vision {
		t.Error("vision should pass")
	}
	if !r.Passed {
		t.Error("gates should pass")
	}
	if !r.Complex {
		t.Error("complex should pass")
	}
	if r.Score <= 0.5 {
		t.Errorf("full pass should score > 0.5, got %.2f", r.Score)
	}
}

func TestRun_VisionFailDoesNotBlockGates(t *testing.T) {
	// Vision fails (wrong color), but gates and complex still run and pass
	turns := []api.Message{
		turn(toolCall("v1", "describe_image", map[string]string{"color": "blue"})), // vision fails
	}
	turns = append(turns, passingGateTurns()...) // 5 gates pass
	turns = append(turns,
		turn(toolCall("c1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("c2", "read_file", map[string]string{"path": "internal/users/service.go"})),
		turn(toolCall("c3", "read_file", map[string]string{"path": "internal/notify/subscriptions.go"})),
		turn(toolCall("c4", "submit_todos", map[string]string{"summary": "subscription not removed on delete", "todos": goodTodos})),
	)
	c := &scriptedClient{turns: turns}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Vision {
		t.Error("vision should fail")
	}
	if !r.Passed {
		t.Error("gates should still pass despite vision failure")
	}
	if !r.Complex {
		t.Error("complex should still pass despite vision failure")
	}
}

func TestRun_VisionTransportErrorIsInconclusive(t *testing.T) {
	c := &erroringClient{} // errors on the very first request (vision)
	r, err := Run(context.Background(), c, "test", "m")
	if err == nil {
		t.Fatal("expected a transport error to be returned")
	}
	if !r.Errored {
		t.Error("vision transport failure must mark the result Errored")
	}
	if r.Passed {
		t.Error("an inconclusive run must not report Passed")
	}
}

// visionErroringClient replays one vision turn, then gates, then errors —
// used to simulate a transport error during the complex stage after vision
// has already run.
type visionErroringClient struct {
	api.ClientInterface
	turns []api.Message
	i     int
}

func (c *visionErroringClient) SendChatRequest(_ context.Context, _ []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	if c.i < len(c.turns) {
		msg := c.turns[c.i]
		c.i++
		if msg.Role == "" {
			msg.Role = "assistant"
		}
		return &api.ChatResponse{Choices: []api.ChatChoice{{Message: msg, FinishReason: "tool_calls"}}}, nil
	}
	return nil, errors.New("simulated transport error")
}

func TestRun_ComplexTransportErrorAfterVision(t *testing.T) {
	// Vision pass + gates pass + complex errors
	turns := []api.Message{
		turn(toolCall("v1", "describe_image", map[string]string{"color": "red"})), // vision pass
	}
	turns = append(turns, passingGateTurns()...) // 5 gates pass
	// Next request (complex stage) will error
	c := &visionErroringClient{turns: turns}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("gates passed, Run should not return an error: %v", err)
	}
	if !r.Errored {
		t.Error("complex-stage transport failure must mark the result Errored")
	}
	if !r.Passed {
		t.Error("gates passed, so Passed should be true")
	}
	if r.Complex {
		t.Error("complex must not be reported true when it could not be assessed")
	}
	if !r.Vision {
		t.Error("vision passed, so Vision should be true")
	}
}
