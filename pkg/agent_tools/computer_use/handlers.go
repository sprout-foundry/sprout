package computer_use

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ---------------------------------------------------------------------------
// Local extraction helpers
// ---------------------------------------------------------------------------

// extractRequiredString extracts a required string from args, returning an error if missing.
func extractRequiredString(args map[string]any, key string) (string, error) {
	val, exists := args[key]
	if !exists || val == nil {
		return "", fmt.Errorf("parameter '%s' is required", key)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter '%s' must be a string, got %T", key, val)
	}
	return s, nil
}

// extractRequiredInt extracts a required int from args (handles float64 from JSON).
func extractRequiredInt(args map[string]any, key string) (int, error) {
	val, exists := args[key]
	if !exists || val == nil {
		return 0, fmt.Errorf("parameter '%s' is required", key)
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("parameter '%s' must be an integer, got %T", key, val)
	}
}

// extractOptionalInt returns an optional int or zero.
func extractOptionalInt(args map[string]any, key string) int {
	val, exists := args[key]
	if !exists || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}

// extractOptionalBool returns an optional bool or false.
func extractOptionalBool(args map[string]any, key string) bool {
	val, exists := args[key]
	if !exists || val == nil {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// parseRegionArg extracts the optional region object from args into a *Rect.
func parseRegionArg(args map[string]any) *Rect {
	regionRaw, ok := args["region"]
	if !ok || regionRaw == nil {
		return nil
	}
	region, ok := regionRaw.(map[string]any)
	if !ok {
		return nil
	}
	x := extractOptionalInt(region, "x")
	y := extractOptionalInt(region, "y")
	w := extractOptionalInt(region, "width")
	h := extractOptionalInt(region, "height")
	if w == 0 && h == 0 {
		return nil
	}
	return &Rect{X: x, Y: y, Width: w, Height: h}
}

// parseMouseButton converts a string to a MouseButton constant.
func parseMouseButton(s string) MouseButton {
	switch strings.ToLower(s) {
	case "right":
		return MouseRight
	case "middle":
		return MouseMiddle
	default:
		return MouseLeft
	}
}

// parseScrollDir converts a string to a ScrollDir constant.
func parseScrollDir(s string) (ScrollDir, error) {
	switch strings.ToLower(s) {
	case "up":
		return ScrollUp, nil
	case "down":
		return ScrollDown, nil
	case "left":
		return ScrollLeft, nil
	case "right":
		return ScrollRight, nil
	default:
		return "", fmt.Errorf("invalid scroll direction: %s", s)
	}
}

// ---------------------------------------------------------------------------
// take_screenshot
// ---------------------------------------------------------------------------

type takeScreenshotHandler struct{}

func (h *takeScreenshotHandler) Name() string {
	return "take_screenshot"
}

func (h *takeScreenshotHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "take_screenshot",
		Description: "Capture a screenshot of the current screen (or a region). Returns the image as base64 for vision-capable models to interpret.",
		Parameters: []tools.ParameterDef{
			{
				Name:        "region",
				Type:        "object",
				Required:    false,
				Description: "Optional region to capture: {x, y, width, height}. Omit to capture full screen.",
			},
		},
	}
}

func (h *takeScreenshotHandler) Validate(_ map[string]any) error {
	return nil
}

func (h *takeScreenshotHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	region := parseRegionArg(args)
	img, dims, err := backend.Screenshot(region)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("screenshot failed: %v", err), IsError: true}, err
	}

	b64 := base64.StdEncoding.EncodeToString(img)
	out, err := json.Marshal(map[string]any{
		"image_base64": b64,
		"width":        dims.Width,
		"height":       dims.Height,
		"display_id":   "default",
	})
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("marshal screenshot result: %v", err), IsError: true}, err
	}

	return tools.ToolResult{
		Output: string(out),
		Images: []tools.ImageData{
			{URI: "data:image/png;base64," + b64, MIMEType: "image/png"},
		},
		StructuredOut: map[string]any{
			"width":      dims.Width,
			"height":     dims.Height,
			"display_id": "default",
		},
	}, nil
}

func (h *takeScreenshotHandler) Aliases() []string             { return nil }
func (h *takeScreenshotHandler) Timeout() time.Duration        { return 0 }
func (h *takeScreenshotHandler) MaxResultSize() int            { return 0 }
func (h *takeScreenshotHandler) SafeForParallel() bool         { return false }
func (h *takeScreenshotHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// mouse_click
// ---------------------------------------------------------------------------

type mouseClickHandler struct{}

func (h *mouseClickHandler) Name() string {
	return "mouse_click"
}

func (h *mouseClickHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "mouse_click",
		Description: "Click the mouse at coordinates (x, y). Supports left, right, middle buttons and double-click.",
		Parameters: []tools.ParameterDef{
			{Name: "x", Type: "integer", Required: true, Description: "X coordinate (pixels from left)"},
			{Name: "y", Type: "integer", Required: true, Description: "Y coordinate (pixels from top)"},
			{Name: "button", Type: "string", Required: false, Description: "Mouse button: left (default), right, middle"},
			{Name: "double", Type: "boolean", Required: false, Description: "Whether to double-click (default: false)"},
		},
		Required: []string{"x", "y"},
	}
}

func (h *mouseClickHandler) Validate(args map[string]any) error {
	if _, err := extractRequiredInt(args, "x"); err != nil {
		return err
	}
	if _, err := extractRequiredInt(args, "y"); err != nil {
		return err
	}
	return nil
}

func (h *mouseClickHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	x, err := extractRequiredInt(args, "x")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	y, err := extractRequiredInt(args, "y")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	button := MouseLeft
	if btn, ok := args["button"].(string); ok && btn != "" {
		button = parseMouseButton(btn)
	}
	double := extractOptionalBool(args, "double")

	err = backend.MouseClick(x, y, button, double)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("mouse click failed: %v", err), IsError: true}, err
	}

	clickType := "single"
	if double {
		clickType = "double"
	}
	return tools.ToolResult{
		Output: fmt.Sprintf("Mouse %s clicked at (%d, %d) with button %s", clickType, x, y, button),
	}, nil
}

func (h *mouseClickHandler) Aliases() []string             { return nil }
func (h *mouseClickHandler) Timeout() time.Duration        { return 0 }
func (h *mouseClickHandler) MaxResultSize() int            { return 0 }
func (h *mouseClickHandler) SafeForParallel() bool         { return false }
func (h *mouseClickHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// mouse_drag
// ---------------------------------------------------------------------------

type mouseDragHandler struct{}

func (h *mouseDragHandler) Name() string {
	return "mouse_drag"
}

func (h *mouseDragHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "mouse_drag",
		Description: "Drag the mouse from (from_x, from_y) to (to_x, to_y) holding a button.",
		Parameters: []tools.ParameterDef{
			{Name: "from_x", Type: "integer", Required: true, Description: "Starting X coordinate"},
			{Name: "from_y", Type: "integer", Required: true, Description: "Starting Y coordinate"},
			{Name: "to_x", Type: "integer", Required: true, Description: "Ending X coordinate"},
			{Name: "to_y", Type: "integer", Required: true, Description: "Ending Y coordinate"},
			{Name: "button", Type: "string", Required: false, Description: "Mouse button: left (default), right, middle"},
		},
		Required: []string{"from_x", "from_y", "to_x", "to_y"},
	}
}

func (h *mouseDragHandler) Validate(args map[string]any) error {
	for _, key := range []string{"from_x", "from_y", "to_x", "to_y"} {
		if _, err := extractRequiredInt(args, key); err != nil {
			return err
		}
	}
	return nil
}

func (h *mouseDragHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	fromX, err := extractRequiredInt(args, "from_x")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	fromY, err := extractRequiredInt(args, "from_y")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	toX, err := extractRequiredInt(args, "to_x")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	toY, err := extractRequiredInt(args, "to_y")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}

	button := MouseLeft
	if btn, ok := args["button"].(string); ok && btn != "" {
		button = parseMouseButton(btn)
	}

	err = backend.MouseDrag(Point{X: fromX, Y: fromY}, Point{X: toX, Y: toY}, button)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("mouse drag failed: %v", err), IsError: true}, err
	}

	return tools.ToolResult{
		Output: fmt.Sprintf("Mouse dragged from (%d, %d) to (%d, %d) with button %s", fromX, fromY, toX, toY, button),
	}, nil
}

func (h *mouseDragHandler) Aliases() []string             { return nil }
func (h *mouseDragHandler) Timeout() time.Duration        { return 0 }
func (h *mouseDragHandler) MaxResultSize() int            { return 0 }
func (h *mouseDragHandler) SafeForParallel() bool         { return false }
func (h *mouseDragHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// keyboard_type
// ---------------------------------------------------------------------------

type keyboardTypeHandler struct{}

func (h *keyboardTypeHandler) Name() string {
	return "keyboard_type"
}

func (h *keyboardTypeHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "keyboard_type",
		Description: "Type a string of text verbatim as if typed on a keyboard.",
		Parameters: []tools.ParameterDef{
			{Name: "text", Type: "string", Required: true, Description: "Text to type"},
		},
		Required: []string{"text"},
	}
}

func (h *keyboardTypeHandler) Validate(args map[string]any) error {
	if _, err := extractRequiredString(args, "text"); err != nil {
		return err
	}
	return nil
}

func (h *keyboardTypeHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	text, err := extractRequiredString(args, "text")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}

	err = backend.KeyboardType(text)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("keyboard type failed: %v", err), IsError: true}, err
	}

	return tools.ToolResult{
		Output: fmt.Sprintf("Typed %d characters", len(text)),
	}, nil
}

func (h *keyboardTypeHandler) Aliases() []string             { return nil }
func (h *keyboardTypeHandler) Timeout() time.Duration        { return 0 }
func (h *keyboardTypeHandler) MaxResultSize() int            { return 0 }
func (h *keyboardTypeHandler) SafeForParallel() bool         { return false }
func (h *keyboardTypeHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// keyboard_press
// ---------------------------------------------------------------------------

type keyboardPressHandler struct{}

func (h *keyboardPressHandler) Name() string {
	return "keyboard_press"
}

func (h *keyboardPressHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "keyboard_press",
		Description: "Press a single special key or key chord. Supports keys like Enter, Tab, Escape, and chords like cmd+space, ctrl+shift+t.",
		Parameters: []tools.ParameterDef{
			{Name: "key", Type: "string", Required: true, Description: "Key name or chord (e.g. Enter, Tab, Escape, cmd+space, ctrl+shift+t)"},
		},
		Required: []string{"key"},
	}
}

func (h *keyboardPressHandler) Validate(args map[string]any) error {
	if _, err := extractRequiredString(args, "key"); err != nil {
		return err
	}
	return nil
}

func (h *keyboardPressHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	key, err := extractRequiredString(args, "key")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}

	err = backend.KeyboardPress(key)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("keyboard press failed: %v", err), IsError: true}, err
	}

	return tools.ToolResult{
		Output: fmt.Sprintf("Pressed key: %s", key),
	}, nil
}

func (h *keyboardPressHandler) Aliases() []string             { return nil }
func (h *keyboardPressHandler) Timeout() time.Duration        { return 0 }
func (h *keyboardPressHandler) MaxResultSize() int            { return 0 }
func (h *keyboardPressHandler) SafeForParallel() bool         { return false }
func (h *keyboardPressHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// scroll
// ---------------------------------------------------------------------------

type scrollHandler struct{}

func (h *scrollHandler) Name() string {
	return "scroll"
}

func (h *scrollHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "scroll",
		Description: "Scroll the screen in a given direction. Optionally at specific coordinates.",
		Parameters: []tools.ParameterDef{
			{Name: "direction", Type: "string", Required: true, Description: "Scroll direction: up, down, left, right"},
			{Name: "amount", Type: "integer", Required: true, Description: "Scroll amount (larger = more scroll)"},
			{Name: "x", Type: "integer", Required: false, Description: "X coordinate to scroll at (optional)"},
			{Name: "y", Type: "integer", Required: false, Description: "Y coordinate to scroll at (optional)"},
		},
		Required: []string{"direction", "amount"},
	}
}

func (h *scrollHandler) Validate(args map[string]any) error {
	dir, err := extractRequiredString(args, "direction")
	if err != nil {
		return err
	}
	if _, err := parseScrollDir(dir); err != nil {
		return err
	}
	if _, err := extractRequiredInt(args, "amount"); err != nil {
		return err
	}
	return nil
}

func (h *scrollHandler) Execute(_ context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	dirStr, err := extractRequiredString(args, "direction")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	dir, err := parseScrollDir(dirStr)
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}
	amount, err := extractRequiredInt(args, "amount")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}

	var at *Point
	x := extractOptionalInt(args, "x")
	y := extractOptionalInt(args, "y")
	if x != 0 || y != 0 {
		at = &Point{X: x, Y: y}
	}

	err = backend.Scroll(dir, amount, at)
	if err != nil {
		return tools.ToolResult{Output: fmt.Sprintf("scroll failed: %v", err), IsError: true}, err
	}

	return tools.ToolResult{
		Output: fmt.Sprintf("Scrolled %s by %d", dir, amount),
	}, nil
}

func (h *scrollHandler) Aliases() []string             { return nil }
func (h *scrollHandler) Timeout() time.Duration        { return 0 }
func (h *scrollHandler) MaxResultSize() int            { return 0 }
func (h *scrollHandler) SafeForParallel() bool         { return false }
func (h *scrollHandler) Interactive() bool             { return false }

// ---------------------------------------------------------------------------
// wait
// ---------------------------------------------------------------------------

const maxWaitMs = 60000 // 60 seconds max

type waitHandler struct{}

func (h *waitHandler) Name() string {
	return "wait"
}

func (h *waitHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "wait",
		Description: "Pause for a specified number of milliseconds to let UI settle. Maximum 60000ms (60s).",
		Parameters: []tools.ParameterDef{
			{Name: "ms", Type: "integer", Required: true, Description: "Milliseconds to wait (1-60000)"},
		},
		Required: []string{"ms"},
	}
}

func (h *waitHandler) Validate(args map[string]any) error {
	ms, err := extractRequiredInt(args, "ms")
	if err != nil {
		return err
	}
	if ms <= 0 || ms > maxWaitMs {
		return fmt.Errorf("parameter 'ms' must be between 1 and %d", maxWaitMs)
	}
	return nil
}

func (h *waitHandler) Execute(ctx context.Context, _ tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	ms, err := extractRequiredInt(args, "ms")
	if err != nil {
		return tools.ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Use context-aware sleep so cancellation works.
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-timer.C:
		return tools.ToolResult{
			Output: fmt.Sprintf("Waited %d ms", ms),
		}, nil
	case <-ctx.Done():
		return tools.ToolResult{
			Output:  fmt.Sprintf("Wait cancelled after %d ms", ms),
			IsError: true,
		}, ctx.Err()
	}
}

func (h *waitHandler) Aliases() []string             { return nil }
func (h *waitHandler) Timeout() time.Duration        { return 0 }
func (h *waitHandler) MaxResultSize() int            { return 0 }
func (h *waitHandler) SafeForParallel() bool         { return false }
func (h *waitHandler) Interactive() bool             { return false }
