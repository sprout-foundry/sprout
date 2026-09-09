package computer_use

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestSmokeRealBackendInput runs a harmless live input sequence against the
// real macOS backend: move cursor, type into the foreground app, press a key,
// scroll. Skips when the platform backend is unavailable.
func TestSmokeRealBackendInput(t *testing.T) {
	real, err := NewPlatformBackend()
	if err != nil {
		t.Skipf("platform backend unavailable on this machine: %v", err)
	}
	SetBackend(real)
	t.Cleanup(func() { SetBackend(&MockBackend{}) })

	find := func(name string) tools.ToolHandler {
		for _, h := range Handlers() {
			if h.Name() == name {
				return h
			}
		}
		t.Fatalf("handler %q not found", name)
		return nil
	}

	env := tools.ToolEnv{}
	ctx := context.Background()

	// 1. Type a character (lands in whatever window is focused).
	if _, err := find("keyboard_type").Execute(ctx, env, map[string]any{"text": " "}); err != nil {
		t.Fatalf("keyboard_type: %v", err)
	}
	t.Log("keyboard_type OK")

	// 2. Press a key.
	if _, err := find("keyboard_press").Execute(ctx, env, map[string]any{"key": "space"}); err != nil {
		t.Fatalf("keyboard_press: %v", err)
	}
	t.Log("keyboard_press OK")

	// 3. Scroll.
	if _, err := find("scroll").Execute(ctx, env, map[string]any{"direction": "down", "amount": 1}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	t.Log("scroll OK")

	// 4. Click at a coordinate over the desktop.
	if _, err := find("mouse_click").Execute(ctx, env, map[string]any{"x": 600, "y": 400}); err != nil {
		t.Fatalf("mouse_click: %v", err)
	}
	t.Log("mouse_click OK")

	// 5. Verify the screenshot path still works post-input.
	var m map[string]any
	out, err := find("take_screenshot").Execute(ctx, env, nil)
	if err != nil {
		t.Fatalf("take_screenshot: %v", err)
	}
	if err := json.Unmarshal([]byte(out.Output), &m); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if w, _ := m["width"].(float64); w <= 1 {
		t.Fatalf("screenshot regression: %v", m)
	}
	if !strings.HasPrefix(m["image_base64"].(string), "iVBORw0KGgo") {
		t.Fatal("not a PNG")
	}
	t.Logf("post-input screenshot OK: %vx%v", m["width"], m["height"])
}
