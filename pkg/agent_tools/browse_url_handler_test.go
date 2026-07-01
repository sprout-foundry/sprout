package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// mockWebBrowserForBrowseURL records the arguments it receives so the
// handler can be tested end-to-end without a real browser.
// ---------------------------------------------------------------------------

type mockWebBrowserForBrowseURL struct {
	lastURL      string
	lastOpts     map[string]any
	returnResult string
	returnError  error
}

func (m *mockWebBrowserForBrowseURL) BrowseURL(_ context.Context, url string, opts map[string]any) (string, error) {
	m.lastURL = url
	m.lastOpts = opts
	if m.returnError != nil {
		return "", m.returnError
	}
	return m.returnResult, nil
}

// ---------------------------------------------------------------------------
// Handler Execute tests (these exercise real handler logic with a mock
// WebBrowser — the adapter is intentionally skipped because it calls
// webcontent.BrowseURL which requires a real browser at runtime).
// ---------------------------------------------------------------------------

func TestBrowseURLHandler_WithBrowser(t *testing.T) {
	t.Parallel()
	mock := &mockWebBrowserForBrowseURL{returnResult: `{"title":"Example"}`}
	h := &browseURLHandler{}

	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	args := map[string]any{
		"url":               "https://example.com",
		"action":            "text",
		"viewport_width":    float64(1920),
		"include_console":   true,
		"extra_ignored_key": "value",
	}

	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireEqual(t, result.IsError, false, "IsError")
	requireEqual(t, result.Output, `{"title":"Example"}`, "Output")

	// Verify the URL was passed through.
	requireEqual(t, mock.lastURL, "https://example.com", "lastURL")

	// Verify opts contains every key except "url".
	requireEqual(t, len(mock.lastOpts), len(args)-1, "opts length")
	if _, ok := mock.lastOpts["url"]; ok {
		t.Error("opts should not contain 'url'")
	}
	for k, v := range args {
		if k == "url" {
			continue
		}
		if mock.lastOpts[k] != v {
			t.Errorf("opts[%q] = %v, want %v", k, mock.lastOpts[k], v)
		}
	}
}

func TestBrowseURLHandler_WithBrowser_EventBus(t *testing.T) {
	t.Parallel()
	mock := &mockWebBrowserForBrowseURL{returnResult: "page"}
	bus := events.NewEventBus()
	h := &browseURLHandler{}

	// Subscribe to capture events (all subscribers get all events).
	ch := bus.Subscribe("test-events")

	ctx := context.Background()
	env := ToolEnv{
		WebBrowser: mock,
		EventBus:   bus,
	}
	args := map[string]any{"url": "http://test.local"}

	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireEqual(t, result.Output, "page", "Output")

	// Collect up to 2 events from the channel (non-blocking drain).
	var eventsReceived []events.UIEvent
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			eventsReceived = append(eventsReceived, ev)
		default:
			break
		}
	}

	if len(eventsReceived) < 2 {
		t.Fatalf("expected 2 events (tool_start + tool_end), got %d", len(eventsReceived))
	}
	if eventsReceived[0].Type != events.EventTypeToolStart {
		t.Errorf("first event type = %q, want %q", eventsReceived[0].Type, events.EventTypeToolStart)
	}
	if eventsReceived[1].Type != events.EventTypeToolEnd {
		t.Errorf("second event type = %q, want %q", eventsReceived[1].Type, events.EventTypeToolEnd)
	}
	// Verify error field is false on success.
	if data, ok := eventsReceived[1].Data.(map[string]any); ok {
		if errVal, hasErr := data["error"]; hasErr {
			if errVal != false {
				t.Errorf("tool_end error field should be false on success, got %v", errVal)
			}
		}
	}
}

func TestBrowseURLHandler_WithBrowser_EventBus_ErrorFlag(t *testing.T) {
	t.Parallel()
	mock := &mockWebBrowserForBrowseURL{
		returnError:  context.DeadlineExceeded,
		returnResult: "",
	}
	bus := events.NewEventBus()
	h := &browseURLHandler{}

	ch := bus.Subscribe("test-events")

	ctx := context.Background()
	env := ToolEnv{
		WebBrowser: mock,
		EventBus:   bus,
	}
	args := map[string]any{"url": "http://slow.local"}

	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError")

	// Collect events.
	var eventsReceived []events.UIEvent
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			eventsReceived = append(eventsReceived, ev)
		default:
			break
		}
	}

	if len(eventsReceived) < 2 {
		t.Fatalf("expected 2 events (tool_start + tool_end), got %d", len(eventsReceived))
	}
	// Verify error field is true on failure.
	if data, ok := eventsReceived[1].Data.(map[string]any); ok {
		if errVal, hasErr := data["error"]; hasErr {
			if errVal != true {
				t.Errorf("tool_end error field should be true on failure, got %v", errVal)
			}
		}
	}
}

func TestBrowseURLHandler_NilBrowser(t *testing.T) {
	t.Parallel()
	h := &browseURLHandler{}
	ctx := context.Background()
	env := ToolEnv{} // WebBrowser is nil
	args := map[string]any{"url": "https://example.com"}

	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError")
	if !strings.Contains(result.Output, "browser not available") {
		t.Errorf("Output should mention 'browser not available', got: %s", result.Output)
	}
}

func TestBrowseURLHandler_InvalidURL(t *testing.T) {
	t.Parallel()
	h := &browseURLHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: &mockWebBrowserForBrowseURL{}}

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing url",
			args:    map[string]any{"action": "text"},
			wantErr: "url",
		},
		{
			name:    "nil args",
			args:    nil,
			wantErr: "url",
		},
		{
			name:    "non-string url",
			args:    map[string]any{"url": 123},
			wantErr: "url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := h.Execute(ctx, env, tc.args)
			requireNoError(t, err)
			requireTrue(t, result.IsError, "IsError")
			if !strings.Contains(result.Output, tc.wantErr) {
				t.Errorf("Output should mention %q, got: %s", tc.wantErr, result.Output)
			}
		})
	}
}

func TestBrowseURLHandler_BrowserError(t *testing.T) {
	t.Parallel()
	mockErr := &mockWebBrowserForBrowseURL{
		returnError:  context.DeadlineExceeded,
		returnResult: "",
	}
	h := &browseURLHandler{}

	ctx := context.Background()
	env := ToolEnv{WebBrowser: mockErr}
	args := map[string]any{"url": "https://slow.example.com", "wait_timeout_ms": float64(500)}

	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError")
	if !strings.Contains(result.Output, "deadline") || !strings.Contains(result.Output, "exceeded") {
		t.Errorf("Output should contain error message, got: %s", result.Output)
	}
}

func TestBrowseURLHandler_EdgeCaseArgs(t *testing.T) {
	t.Parallel()
	// Verify that the handler passes through all non-url args without
	// filtering or modifying them — the adapter (or whatever WebBrowser
	// implementation is in use) is responsible for field extraction.
	mock := &mockWebBrowserForBrowseURL{returnResult: "ok"}
	h := &browseURLHandler{}

	// Construct args the way the LLM would: float64 for JSON numbers,
	// bool for booleans, string for strings, []interface{} for arrays.
	args := map[string]any{
		"url":               "http://localhost:3000",
		"action":            "DOM", // mixed case
		"viewport_width":    float64(1280),   // JSON float64
		"viewport_height":   float64(720),
		"user_agent":        "Mozilla/5.0",
		"screenshot_path":   "/tmp/screen.png",
		"session_id":        "sess-123",
		"persist_session":   true,
		"close_session":     false,
		"wait_for_selector": "#main-content",
		"wait_timeout_ms":   float64(15000),
		"capture_dom":       true,
		"capture_text":      true,
		"include_console":   true,
		"capture_network":   true,
		"capture_storage":   true,
		"capture_cookies":   true,
		"response_max_chars": float64(5000),
		"capture_selectors": []interface{}{
			"#header",
			"#footer",
			"  ", // whitespace-only (should be filtered by adapter)
		},
		"steps": []interface{}{
			map[string]interface{}{
				"action":   "click",
				"selector": "#submit-btn",
			},
			map[string]interface{}{
				"action":   "wait_for",
				"selector": "#result",
			},
		},
	}

	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	result, err := h.Execute(ctx, env, args)
	requireNoError(t, err)
	requireEqual(t, result.Output, "ok", "Output")

	// The handler should pass every non-"url" key through.
	for k, v := range args {
		if k == "url" {
			continue
		}
		if !reflect.DeepEqual(mock.lastOpts[k], v) {
			t.Errorf("opts[%q] = %v (type %T), want %v (type %T)", k, mock.lastOpts[k], mock.lastOpts[k], v, v)
		}
	}
}

// ---------------------------------------------------------------------------
// Adapter field extraction tests (these test the real buildBrowseOptions
// function to verify every map[string]any key maps to the correct
// BrowseOptions field).
// ---------------------------------------------------------------------------

func TestBuildBrowseOptions_BasicFields(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"action":          "dom",
		"viewport_width":  float64(1920),
		"viewport_height": float64(1080),
		"user_agent":      "CustomBot/1.0",
		"screenshot_path": "/tmp/shot.png",
	}

	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireEqual(t, got.Action, "dom", "Action")
	requireEqual(t, got.ViewportWidth, 1920, "ViewportWidth")
	requireEqual(t, got.ViewportHeight, 1080, "ViewportHeight")
	requireEqual(t, got.UserAgent, "CustomBot/1.0", "UserAgent")
	requireEqual(t, got.ScreenshotPath, "/tmp/shot.png", "ScreenshotPath")
}

func TestBuildBrowseOptions_ActionNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase", "text", "text"},
		{"uppercase", "TEXT", "text"},
		{"mixed case", "DoM", "dom"},
		{"screenshot upper", "SCREENSHOT", "screenshot"},
		{"inspect", "Inspect", "inspect"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := map[string]any{"action": tc.in}
			got, err := buildBrowseOptions(opts)
			requireNoError(t, err)
			requireEqual(t, got.Action, tc.want, "Action")
		})
	}
}

func TestBuildBrowseOptions_DefaultAction(t *testing.T) {
	t.Parallel()
	// No action key at all
	got, err := buildBrowseOptions(map[string]any{})
	requireNoError(t, err)
	requireEqual(t, got.Action, "text", "Action (default)")

	// Empty string action
	got, err = buildBrowseOptions(map[string]any{"action": ""})
	requireNoError(t, err)
	requireEqual(t, got.Action, "text", "Action (empty → default)")
}

func TestBuildBrowseOptions_SessionOptions(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"session_id":      "my-session",
		"persist_session": true,
		"close_session":   true,
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireEqual(t, got.SessionID, "my-session", "SessionID")
	requireTrue(t, got.PersistSession, "PersistSession")
	requireTrue(t, got.CloseSession, "CloseSession")
}

func TestBuildBrowseOptions_WaitOptions(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"wait_for_selector": "#main-content",
		"wait_timeout_ms":  float64(30000),
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireEqual(t, got.WaitForSelector, "#main-content", "WaitForSelector")
	requireEqual(t, got.WaitTimeoutMs, 30000, "WaitTimeoutMs")
}

func TestBuildBrowseOptions_CaptureOptions(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"capture_dom":        true,
		"capture_text":       true,
		"include_console":    true,
		"capture_network":    true,
		"capture_storage":    true,
		"capture_cookies":    true,
		"response_max_chars": float64(10000),
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireTrue(t, got.CaptureDOM, "CaptureDOM")
	requireTrue(t, got.CaptureText, "CaptureText")
	requireTrue(t, got.IncludeConsole, "IncludeConsole")
	requireTrue(t, got.CaptureNetwork, "CaptureNetwork")
	requireTrue(t, got.CaptureStorage, "CaptureStorage")
	requireTrue(t, got.CaptureCookies, "CaptureCookies")
	requireEqual(t, got.ResponseMaxChars, 10000, "ResponseMaxChars")
}

func TestBuildBrowseOptions_CaptureSelectors(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"capture_selectors": []interface{}{
			"#header",
			".nav-link",
			"  ", // whitespace-only → should be filtered
			"",   // empty → should be filtered
			"#footer",
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	if len(got.CaptureSelectors) != 3 {
		t.Fatalf("CaptureSelectors length = %d, want 3", len(got.CaptureSelectors))
	}
	requireEqual(t, got.CaptureSelectors[0], "#header", "CaptureSelectors[0]")
	requireEqual(t, got.CaptureSelectors[1], ".nav-link", "CaptureSelectors[1]")
	requireEqual(t, got.CaptureSelectors[2], "#footer", "CaptureSelectors[2]")
}

func TestBuildBrowseOptions_CaptureSelectorsMissing(t *testing.T) {
	t.Parallel()
	got, err := buildBrowseOptions(map[string]any{})
	requireNoError(t, err)
	if got.CaptureSelectors != nil {
		t.Errorf("CaptureSelectors should be nil when not provided, got %v", got.CaptureSelectors)
	}
}

func TestBuildBrowseOptions_Steps(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"steps": []interface{}{
			map[string]interface{}{
				"action":   "click",
				"selector": "#btn",
			},
			map[string]interface{}{
				"action":   "fill",
				"selector": "#input",
				"value":    "hello",
			},
			map[string]interface{}{
				"action":   "wait_for",
				"selector": "#result",
			},
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireEqual(t, len(got.Steps), 3, "Steps length")
	requireEqual(t, got.Steps[0].Action, "click", "Steps[0].Action")
	requireEqual(t, got.Steps[0].Selector, "#btn", "Steps[0].Selector")
	requireEqual(t, got.Steps[1].Action, "fill", "Steps[1].Action")
	requireEqual(t, got.Steps[1].Value, "hello", "Steps[1].Value")
	requireEqual(t, got.Steps[2].Selector, "#result", "Steps[2].Selector")
}

func TestBuildBrowseOptions_StepsMissing(t *testing.T) {
	t.Parallel()
	got, err := buildBrowseOptions(map[string]any{})
	requireNoError(t, err)
	if got.Steps != nil {
		t.Errorf("Steps should be nil when not provided, got %v", got.Steps)
	}
}

func TestBuildBrowseOptions_AllFieldsCombined(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"action":            "INSPECT",
		"viewport_width":    float64(1920),
		"viewport_height":   float64(1080),
		"user_agent":        "TestBot",
		"screenshot_path":   "/tmp/inspect.png",
		"session_id":        "sess-42",
		"persist_session":   true,
		"close_session":     false,
		"wait_for_selector": "#content",
		"wait_timeout_ms":   float64(20000),
		"capture_dom":       true,
		"capture_text":      true,
		"include_console":   true,
		"capture_network":   true,
		"capture_storage":   true,
		"capture_cookies":   true,
		"response_max_chars": float64(8000),
		"capture_selectors": []interface{}{"#header", "#footer"},
		"steps": []interface{}{
			map[string]interface{}{"action": "click", "selector": "#start"},
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)

	requireEqual(t, got.Action, "inspect", "Action (normalized)")
	requireEqual(t, got.ViewportWidth, 1920, "ViewportWidth")
	requireEqual(t, got.ViewportHeight, 1080, "ViewportHeight")
	requireEqual(t, got.UserAgent, "TestBot", "UserAgent")
	requireEqual(t, got.ScreenshotPath, "/tmp/inspect.png", "ScreenshotPath")
	requireEqual(t, got.SessionID, "sess-42", "SessionID")
	requireTrue(t, got.PersistSession, "PersistSession")
	requireFalse(t, got.CloseSession, "CloseSession")
	requireEqual(t, got.WaitForSelector, "#content", "WaitForSelector")
	requireEqual(t, got.WaitTimeoutMs, 20000, "WaitTimeoutMs")
	requireTrue(t, got.CaptureDOM, "CaptureDOM")
	requireTrue(t, got.CaptureText, "CaptureText")
	requireTrue(t, got.IncludeConsole, "IncludeConsole")
	requireTrue(t, got.CaptureNetwork, "CaptureNetwork")
	requireTrue(t, got.CaptureStorage, "CaptureStorage")
	requireTrue(t, got.CaptureCookies, "CaptureCookies")
	requireEqual(t, got.ResponseMaxChars, 8000, "ResponseMaxChars")
	requireEqual(t, len(got.CaptureSelectors), 2, "CaptureSelectors length")
	requireEqual(t, len(got.Steps), 1, "Steps length")
	requireEqual(t, got.Steps[0].Action, "click", "Steps[0].Action")
}

func TestBuildBrowseOptions_InvalidSteps(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"steps": []interface{}{
			"not-an-object", // invalid step
		},
	}
	_, err := buildBrowseOptions(opts)
	requireNotNil(t, err, "error")
	if !strings.Contains(err.Error(), "browse steps") {
		t.Errorf("Error should mention browse steps, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseBrowseSteps tests (direct unit tests of the package-private
// conversion function).
// ---------------------------------------------------------------------------

func TestParseBrowseSteps_ValidSteps(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"action":   "click",
			"selector": "#submit",
		},
		map[string]interface{}{
			"action":   "type",
			"selector": "#input",
			"value":    "hello world",
		},
		map[string]interface{}{
			"action": "sleep",
			"millis": 1000,
		},
	}

	steps, err := parseBrowseSteps(raw)
	requireNoError(t, err)
	requireEqual(t, len(steps), 3, "Steps length")
	requireEqual(t, steps[0].Action, "click", "Steps[0].Action")
	requireEqual(t, steps[0].Selector, "#submit", "Steps[0].Selector")
	requireEqual(t, steps[1].Action, "type", "Steps[1].Action")
	requireEqual(t, steps[1].Value, "hello world", "Steps[1].Value")
	requireEqual(t, steps[2].Action, "sleep", "Steps[2].Action")
	requireEqual(t, steps[2].Millis, 1000, "Steps[2].Millis")
}

func TestParseBrowseSteps_WithAllFields(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"action":   "eval",
			"script":   "return document.title",
			"expect":   "Home",
			"selector": "#page",
			"value":    "",
			"key":      "Enter",
			"millis":   500,
		},
	}

	steps, err := parseBrowseSteps(raw)
	requireNoError(t, err)
	requireEqual(t, len(steps), 1, "Steps length")
	requireEqual(t, steps[0].Action, "eval", "Action")
	requireEqual(t, steps[0].Script, "return document.title", "Script")
	requireEqual(t, steps[0].Expect, "Home", "Expect")
	requireEqual(t, steps[0].Key, "Enter", "Key")
	requireEqual(t, steps[0].Millis, 500, "Millis")
}

func TestParseBrowseSteps_EmptySlice(t *testing.T) {
	t.Parallel()
	raw := []interface{}{}
	steps, err := parseBrowseSteps(raw)
	requireNoError(t, err)
	requireEqual(t, len(steps), 0, "Steps should be empty, not nil")
}

func TestParseBrowseSteps_NonObjectStep(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		"not-an-object",
	}
	_, err := parseBrowseSteps(raw)
	requireNotNil(t, err, "error")
	if !strings.Contains(err.Error(), "must be an object") {
		t.Errorf("Error should mention 'must be an object', got: %v", err)
	}
}

func TestParseBrowseSteps_MissingAction(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"selector": "#btn",
			"value":    "click me",
		},
	}
	_, err := parseBrowseSteps(raw)
	requireNotNil(t, err, "error")
	if !strings.Contains(err.Error(), "requires action") {
		t.Errorf("Error should mention 'requires action', got: %v", err)
	}
}

func TestParseBrowseSteps_EmptyAction(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"action": "",
		},
	}
	_, err := parseBrowseSteps(raw)
	requireNotNil(t, err, "error")
	if !strings.Contains(err.Error(), "requires action") {
		t.Errorf("Error should mention 'requires action', got: %v", err)
	}
}

func TestParseBrowseSteps_WhitespaceOnlyAction(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"action": "   ",
		},
	}
	_, err := parseBrowseSteps(raw)
	requireNotNil(t, err, "error")
	if !strings.Contains(err.Error(), "requires action") {
		t.Errorf("Error should mention 'requires action', got: %v", err)
	}
}

func TestParseBrowseSteps_MixedValidInvalid(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"action":   "click",
			"selector": "#first",
		},
		map[string]interface{}{
			"selector": "#second", // missing action
		},
	}
	_, err := parseBrowseSteps(raw)
	requireNotNil(t, err, "error")
	// Error should reference the second step (index 1).
	if !strings.Contains(err.Error(), "steps[1]") {
		t.Errorf("Error should reference steps[1], got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewBrowserAdapter tests
// ---------------------------------------------------------------------------

func TestNewBrowserAdapter_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	wb := NewBrowserAdapter()
	requireNotNil(t, wb, "WebBrowser returned by NewBrowserAdapter")
}

func TestNewBrowserAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	// Compile-time check that NewBrowserAdapter returns a WebBrowser.
	var _ WebBrowser = NewBrowserAdapter()
}

func TestBrowseURLHandler_ScreenshotWithoutPath(t *testing.T) {
	t.Parallel()
	mock := &mockWebBrowserForBrowseURL{
		returnResult: "page content",
	}
	h := &browseURLHandler{}
	env := ToolEnv{WebBrowser: mock}
	result, err := h.Execute(context.Background(), env, map[string]any{
		"url":    "https://example.com",
		"action": "screenshot",
	})
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError")
	if !strings.Contains(result.Output, "screenshot_path") {
		t.Errorf("expected screenshot_path in Output, got: %s", result.Output)
	}
}

func TestBrowseURLHandler_ScreenshotWithPath(t *testing.T) {
	t.Parallel()
	mock := &mockWebBrowserForBrowseURL{
		returnResult: "screenshot saved",
	}
	h := &browseURLHandler{}
	env := ToolEnv{WebBrowser: mock}
	result, err := h.Execute(context.Background(), env, map[string]any{
		"url":             "https://example.com",
		"action":          "screenshot",
		"screenshot_path": "/tmp/test-shot.png",
	})
	requireNoError(t, err)
	requireFalse(t, result.IsError, "IsError should be false")
	requireEqual(t, result.Output, "screenshot saved", "Output")
	requireEqual(t, mock.lastURL, "https://example.com", "URL")
	requireEqual(t, mock.lastOpts["action"], "screenshot", "action in opts")
	requireEqual(t, mock.lastOpts["screenshot_path"], "/tmp/test-shot.png", "screenshot_path in opts")
}

func TestBrowserAdapter_NilOpts(t *testing.T) {
	t.Parallel()
	result, err := buildBrowseOptions(nil)
	requireNoError(t, err)
	if result.Action != "text" {
		t.Errorf("default action should be 'text', got: %q", result.Action)
	}
}

func TestBuildBrowseOptions_Cookies(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"cookies": map[string]interface{}{
			"session": "abc123",
			"token":   "xyz",
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)
	requireEqual(t, len(got.Cookies), 2, "Cookies length")
	requireEqual(t, got.Cookies["session"], "abc123", "Cookies[session]")
	requireEqual(t, got.Cookies["token"], "xyz", "Cookies[token]")
}

func TestBuildBrowseOptions_Headers(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"headers": map[string]interface{}{
			"Authorization": "Bearer mytoken",
			"X-API-Key":     "secret",
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)
	requireEqual(t, len(got.Headers), 2, "Headers length")
	requireEqual(t, got.Headers["Authorization"], "Bearer mytoken", "Headers[Authorization]")
	requireEqual(t, got.Headers["X-API-Key"], "secret", "Headers[X-API-Key]")
}

func TestBuildBrowseOptions_AllowFileURL(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"allow_file_url": true,
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)
	requireTrue(t, got.AllowFileURL, "AllowFileURL")

	// Default should be false
	got2, err := buildBrowseOptions(map[string]any{})
	requireNoError(t, err)
	requireFalse(t, got2.AllowFileURL, "AllowFileURL default")
}

func TestBuildBrowseOptions_StepsWithNewFields(t *testing.T) {
	t.Parallel()
	opts := map[string]any{
		"steps": []interface{}{
			map[string]interface{}{
				"action":        "wait_for_function",
				"script":        "() => document.readyState === 'complete'",
			},
			map[string]interface{}{
				"action":         "screenshot_selector",
				"selector":       "#chart",
				"screenshot_path": "/tmp/chart.png",
			},
		},
	}
	got, err := buildBrowseOptions(opts)
	requireNoError(t, err)
	requireEqual(t, len(got.Steps), 2, "Steps length")
	requireEqual(t, got.Steps[0].Action, "wait_for_function", "Steps[0].Action")
	requireEqual(t, got.Steps[0].Script, "() => document.readyState === 'complete'", "Steps[0].Script")
	requireEqual(t, got.Steps[1].Action, "screenshot_selector", "Steps[1].Action")
	requireEqual(t, got.Steps[1].Selector, "#chart", "Steps[1].Selector")
	requireEqual(t, got.Steps[1].ScreenshotPath, "/tmp/chart.png", "Steps[1].ScreenshotPath")
}

// ---------------------------------------------------------------------------
// Minimal test helpers to avoid testify dependency.
// ---------------------------------------------------------------------------

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireNotNil(t *testing.T, v interface{}, msg string) {
	t.Helper()
	if v == nil {
		t.Fatalf("%s should not be nil", msg)
	}
}

func requireEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func requireTrue(t *testing.T, v bool, msg string) {
	t.Helper()
	if !v {
		t.Fatalf("%s should be true", msg)
	}
}

func requireFalse(t *testing.T, v bool, msg string) {
	t.Helper()
	if v {
		t.Fatalf("%s should be false", msg)
	}
}
