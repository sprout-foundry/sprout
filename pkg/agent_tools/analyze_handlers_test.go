package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ============================================================================
// SP-079-2: analyze_image_content handler tests
// ============================================================================

func TestAnalyzeImageContent_NilVisionProcessor(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	env := ToolEnv{} // VisionProcessor is nil by default
	args := map[string]any{"image_path": "/tmp/test.png"}

	result, err := h.Execute(ctx, env, args)

	// After removing the redundant nil-VisionProcessor check, the handler
	// delegates to AnalyzeImage() which does its own HasVisionCapability()
	// check internally. AnalyzeImage always returns (json_string, nil) — it
	// encodes errors inside the JSON payload. The handler sees err == nil
	// and sets succeeded = true / IsError = false.
	require.NoError(t, err)
	require.False(t, result.IsError)

	// The key SP-079-2 check: the handler must NOT return its own
	// "vision processor not available" error — that's AnalyzeImage's job.
	require.NotContains(t, result.Output, "vision processor not available")
	require.NotContains(t, result.Output, "requires full")

	// Output should be structured JSON with the input path echoed back.
	require.Contains(t, result.Output, "input_path")
}

func TestAnalyzeImageContent_MissingImagePath(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	env := ToolEnv{}

	// Missing image_path entirely.
	result, err := h.Execute(ctx, env, map[string]any{})
	require.Error(t, err)
	require.True(t, result.IsError)
	require.Contains(t, err.Error(), "image_path")

	// Wrong type for image_path.
	result2, err2 := h.Execute(ctx, env, map[string]any{"image_path": 123})
	require.Error(t, err2)
	require.True(t, result2.IsError)
	require.Contains(t, err2.Error(), "string")
}

func TestAnalyzeImageContent_WithVisionProcessorButNoProvider(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	env := ToolEnv{
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}
	args := map[string]any{"image_path": "/tmp/test.png"}

	result, err := h.Execute(ctx, env, args)

	// AnalyzeImage always returns (json_string, nil) — it encodes errors
	// inside the JSON payload. The handler sees err == nil and sets
	// succeeded = true / IsError = false.
	require.NoError(t, err)
	require.False(t, result.IsError)

	// The key SP-079-2 check: old stub text must not appear.
	require.NotContains(t, result.Output, "requires full *Agent refactoring")
	require.NotContains(t, result.Output, "stub")

	// Output should be structured JSON with the input path echoed back.
	require.Contains(t, result.Output, "input_path")
}

func TestAnalyzeImageContent_EventBusPublished(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus: bus,
		// VisionProcessor nil → AnalyzeImage returns (json, nil) with
		// VISION_NOT_AVAILABLE. Handler sees err == nil → succeeded = true.
	}
	args := map[string]any{"image_path": "/tmp/test.png"}

	_, _ = h.Execute(ctx, env, args)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestAnalyzeImageContent_EventBusSuccessPath(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:        bus,
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}
	// Non-HTML path — reaches AnalyzeImage which returns (json, nil).
	// Handler sees no Go error → succeeded = true.
	args := map[string]any{"image_path": "/tmp/test.png"}

	_, _ = h.Execute(ctx, env, args)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestAnalyzeImageContent_EventBusNil(t *testing.T) {
	t.Parallel()
	h := &analyzeImageContentHandler{}
	ctx := context.Background()

	// No EventBus — handler should not panic.
	env := ToolEnv{}

	_, err := h.Execute(ctx, env, map[string]any{"image_path": "/tmp/test.png"})
	// AnalyzeImage returns (json, nil) even when vision is unavailable,
	// so the handler returns no Go error.
	require.NoError(t, err)
}

// ============================================================================
// SP-079-2: analyze_ui_screenshot handler tests
// ============================================================================

func TestAnalyzeUIScreenshot_NilVisionProcessor(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	env := ToolEnv{} // VisionProcessor is nil by default
	args := map[string]any{"image_path": "/tmp/screenshot.png"}

	result, err := h.Execute(ctx, env, args)

	// After removing the redundant nil-VisionProcessor check, the handler
	// delegates to AnalyzeImage() which does its own HasVisionCapability()
	// check internally. AnalyzeImage always returns (json_string, nil) — it
	// encodes errors inside the JSON payload. The handler sees err == nil
	// and sets succeeded = true / IsError = false.
	require.NoError(t, err)
	require.False(t, result.IsError)

	// The key SP-079-2 check: the handler must NOT return its own
	// "vision processor not available" error — that's AnalyzeImage's job.
	require.NotContains(t, result.Output, "vision processor not available")
	require.NotContains(t, result.Output, "requires full")

	// Output should be structured JSON with the input path echoed back.
	require.Contains(t, result.Output, "input_path")
}

func TestAnalyzeUIScreenshot_MissingImagePath(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	env := ToolEnv{}

	// Missing image_path entirely.
	result, err := h.Execute(ctx, env, map[string]any{})
	require.Error(t, err)
	require.True(t, result.IsError)
	require.Contains(t, err.Error(), "image_path")

	// Wrong type for image_path.
	result2, err2 := h.Execute(ctx, env, map[string]any{"image_path": 456})
	require.Error(t, err2)
	require.True(t, result2.IsError)
	require.Contains(t, err2.Error(), "string")
}

func TestAnalyzeUIScreenshot_HTMLInputDetected(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	env := ToolEnv{
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}

	// Local .html extension — IsHTMLInput checks the extension without I/O.
	result, err := h.Execute(ctx, env, map[string]any{
		"image_path": "/tmp/page.html",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "html content requires browser rendering")
	require.True(t, result.IsError)
	require.Contains(t, result.Output, "HTML content requires a browser")

	// Also verify .htm extension is detected.
	result2, err2 := h.Execute(ctx, env, map[string]any{
		"image_path": "/var/www/index.htm",
	})
	require.Error(t, err2)
	require.True(t, result2.IsError)
	require.Contains(t, result2.Output, "HTML content requires a browser")

	// A non-HTML path should NOT trigger the browser error.
	result3, err3 := h.Execute(ctx, env, map[string]any{
		"image_path": "/tmp/screenshot.png",
	})
	// err3 is nil because AnalyzeImage returns (json, nil) for non-HTML input.
	require.NoError(t, err3)
	require.False(t, result3.IsError)
	require.NotContains(t, result3.Output, "HTML content requires a browser")
}

func TestAnalyzeUIScreenshot_WithVisionProcessorButNoProvider(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	env := ToolEnv{
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}
	args := map[string]any{"image_path": "/tmp/screenshot.png"}

	result, err := h.Execute(ctx, env, args)

	// AnalyzeImage returns (json_string, nil) — same as analyze_image_content.
	require.NoError(t, err)
	require.False(t, result.IsError)

	// The key SP-079-2 check: old stub text must not appear.
	require.NotContains(t, result.Output, "requires full *Agent refactoring")
	require.NotContains(t, result.Output, "stub")

	// Output should be structured JSON.
	require.Contains(t, result.Output, "input_path")
}

func TestAnalyzeUIScreenshot_EventBusPublished(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus: bus,
		// VisionProcessor nil → AnalyzeImage returns (json, nil) with
		// VISION_NOT_AVAILABLE. Handler sees err == nil → succeeded = true.
	}
	args := map[string]any{"image_path": "/tmp/screenshot.png"}

	_, _ = h.Execute(ctx, env, args)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestAnalyzeUIScreenshot_EventBusSuccessPath(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:        bus,
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}
	args := map[string]any{"image_path": "/tmp/screenshot.png"}

	_, _ = h.Execute(ctx, env, args)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestAnalyzeUIScreenshot_EventBusNil(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	// No EventBus — handler should not panic.
	env := ToolEnv{}

	_, err := h.Execute(ctx, env, map[string]any{"image_path": "/tmp/screenshot.png"})
	// AnalyzeImage returns (json, nil) even when vision is unavailable,
	// so the handler returns no Go error.
	require.NoError(t, err)
}

func TestAnalyzeUIScreenshot_HTMLInputViaEventBus(t *testing.T) {
	t.Parallel()
	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:        bus,
		VisionProcessor: NewVisionProcessor(nil, nil, false),
	}
	// HTML path triggers browser-required error before AnalyzeImage is called.
	_, _ = h.Execute(ctx, env, map[string]any{"image_path": "/tmp/page.html"})

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}
