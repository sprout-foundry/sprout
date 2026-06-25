package computer_use

import (
	"context"
	"strings"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// withMock installs a fresh MockBackend, returns it, and restores the previous
// backend when the test ends.
func withMock(t *testing.T) *MockBackend {
	t.Helper()
	prev := GetBackend()
	m := &MockBackend{}
	SetBackend(m)
	t.Cleanup(func() { SetBackend(prev) })
	return m
}

func TestHandlers_AllRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, h := range Handlers() {
		got[h.Name()] = true
	}
	for _, want := range []string{
		"take_screenshot", "mouse_click", "mouse_drag",
		"keyboard_type", "keyboard_press", "scroll", "wait",
	} {
		if !got[want] {
			t.Errorf("Handlers() missing %q", want)
		}
	}
	if len(got) != len(Handlers()) {
		t.Errorf("duplicate handler names in Handlers()")
	}
}

func TestTakeScreenshot_ReturnsImageAndDims(t *testing.T) {
	m := withMock(t)
	m.OverrideScreenshotData = minimalPNG
	m.OverrideScreenshotDims = Size{Width: 1280, Height: 800}

	h := &takeScreenshotHandler{}
	res, err := h.Execute(context.Background(), tools.ToolEnv{}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(res.Images))
	}
	if !strings.HasPrefix(res.Images[0].URI, "data:image/png;base64,") {
		t.Errorf("image URI not a png data uri: %q", res.Images[0].URI[:20])
	}
	if !strings.Contains(res.Output, "1280") || !strings.Contains(res.Output, "800") {
		t.Errorf("output missing dims: %q", res.Output)
	}
}

func TestMouseClick_RecordsButtonAndDouble(t *testing.T) {
	m := withMock(t)
	h := &mouseClickHandler{}
	_, err := h.Execute(context.Background(), tools.ToolEnv{}, map[string]any{
		"x": float64(42), "y": float64(99), "button": "right", "double": true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(m.Records) != 1 || m.Records[0].Action != "MouseClick" {
		t.Fatalf("expected one MouseClick record, got %+v", m.Records)
	}
	r := m.Records[0].Args
	if r["x"] != 42 || r["y"] != 99 || r["button"] != MouseRight || r["double"] != true {
		t.Errorf("unexpected click args: %+v", r)
	}
}

func TestMouseClick_ValidationRequiresCoords(t *testing.T) {
	h := &mouseClickHandler{}
	if err := h.Validate(map[string]any{"y": float64(1)}); err == nil {
		t.Error("expected validation error for missing x")
	}
}

func TestKeyboardPress_PassesKey(t *testing.T) {
	m := withMock(t)
	h := &keyboardPressHandler{}
	if _, err := h.Execute(context.Background(), tools.ToolEnv{}, map[string]any{"key": "ctrl+shift+t"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if m.Records[0].Args["key"] != "ctrl+shift+t" {
		t.Errorf("key not passed through: %+v", m.Records[0].Args)
	}
}

func TestScroll_RejectsBadDirection(t *testing.T) {
	h := &scrollHandler{}
	if err := h.Validate(map[string]any{"direction": "sideways", "amount": float64(3)}); err == nil {
		t.Error("expected validation error for bad direction")
	}
}

func TestWait_ValidatesBounds(t *testing.T) {
	h := &waitHandler{}
	if err := h.Validate(map[string]any{"ms": float64(0)}); err == nil {
		t.Error("expected error for ms=0")
	}
	if err := h.Validate(map[string]any{"ms": float64(maxWaitMs + 1)}); err == nil {
		t.Error("expected error for ms over max")
	}
	if err := h.Validate(map[string]any{"ms": float64(50)}); err != nil {
		t.Errorf("unexpected error for valid ms: %v", err)
	}
}

func TestWait_HonorsContextCancellation(t *testing.T) {
	h := &waitHandler{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := h.Execute(ctx, tools.ToolEnv{}, map[string]any{"ms": float64(5000)})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !res.IsError {
		t.Error("expected IsError on cancellation")
	}
}

func TestWait_Completes(t *testing.T) {
	h := &waitHandler{}
	start := time.Now()
	if _, err := h.Execute(context.Background(), tools.ToolEnv{}, map[string]any{"ms": float64(10)}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("wait returned too fast")
	}
}
