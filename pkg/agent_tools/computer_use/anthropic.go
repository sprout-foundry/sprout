package computer_use

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// AnthropicComputerToolName is the name of Anthropic's native computer use tool.
const AnthropicComputerToolName = "computer_20241022"

// TranslateAnthropicAction converts an Anthropic computer_20241022 action into
// calls to our ComputerBackend interface.
//
// Anthropic's tool uses a single "action" parameter with sub-params:
//
//	action: "screenshot", "mouse_move", "left_click", "right_click", "middle_click",
//	  "double_click", "triple_click", "left_click_drag", "type", "key", "hold_key",
//	  "scroll", "wait"
//	coordinate:       [x, y] for mouse actions
//	to_coordinate:    [x, y] for drag end point
//	text:             for type action
//	key:              for key / hold_key actions
//	scroll_direction: "up", "down", "left", "right" for scroll
//	amount:           for scroll
//	milliseconds:     for wait
//
// The return value is optional structured output (e.g. screenshot data for the
// "screenshot" action). Callers should inspect it for action-specific results.
func TranslateAnthropicAction(action string, params map[string]any) (any, error) {
	action = strings.ToLower(strings.TrimSpace(action))

	switch action {
	case "screenshot":
		return doScreenshot(backend, params)

	case "mouse_move":
		// No backend method for raw cursor move yet. Document the limitation.
		// A future ComputerBackend version may add MoveTo(x, y) error.
		return nil, nil

	case "left_click":
		return doClick(backend, params, MouseLeft, false)

	case "right_click":
		return nil, doClickErr(backend, params, MouseRight, false)

	case "middle_click":
		return nil, doClickErr(backend, params, MouseMiddle, false)

	case "double_click":
		return nil, doClickErr(backend, params, MouseLeft, true)

	case "triple_click":
		// Limitation: we only have single/double. Approximate with a single
		// double-click. A real triple-click requires a separate backend method.
		return nil, doClickErr(backend, params, MouseLeft, true)

	case "left_click_drag":
		return nil, doDrag(backend, params, MouseLeft)

	case "type":
		return nil, doType(backend, params)

	case "key":
		return nil, doKey(backend, params)

	case "hold_key":
		// Limitation: we have no hold/release pair. Map to a single press.
		// A real hold_key requires HoldKey(key)/ReleaseKey(key) backend methods.
		return nil, doKey(backend, params)

	case "scroll":
		return nil, doScroll(backend, params)

	case "wait":
		return nil, doWait(params)

	default:
		return nil, fmt.Errorf("unknown anthropic computer action: %s", action)
	}
}

// ---------------------------------------------------------------------------
// Action dispatchers
// ---------------------------------------------------------------------------

func doScreenshot(b ComputerBackend, params map[string]any) (any, error) {
	img, dims, err := b.Screenshot(nil)
	if err != nil {
		return nil, fmt.Errorf("screenshot failed: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(img)
	return map[string]any{
		"image_base64": b64,
		"width":        dims.Width,
		"height":       dims.Height,
		"data_uri":     "data:image/png;base64," + b64,
	}, nil
}

func doClick(b ComputerBackend, params map[string]any, button MouseButton, double bool) (any, error) {
	err := doClickErr(b, params, button, double)
	return nil, err
}

func doClickErr(b ComputerBackend, params map[string]any, button MouseButton, double bool) error {
	coord, err := extractCoordinate(params, "coordinate")
	if err != nil {
		return err
	}
	return b.MouseClick(coord.X, coord.Y, button, double)
}

func doDrag(b ComputerBackend, params map[string]any, button MouseButton) error {
	from, err := extractCoordinate(params, "coordinate")
	if err != nil {
		return err
	}
	// Anthropic's left_click_drag uses coordinate as the end point with an
	// implicit start. Without persistent cursor state we approximate by
	// reading to_coordinate if supplied, otherwise treat coordinate as both
	// start and end (no-op drag).
	to, err := extractCoordinate(params, "to_coordinate")
	if err != nil {
		return fmt.Errorf("'to_coordinate' parameter is required for drag action")
	}
	return b.MouseDrag(*from, *to, button)
}

func doType(b ComputerBackend, params map[string]any) error {
	text, ok := params["text"].(string)
	if !ok {
		return fmt.Errorf("'text' parameter is required for type action")
	}
	return b.KeyboardType(text)
}

func doKey(b ComputerBackend, params map[string]any) error {
	key, ok := params["key"].(string)
	if !ok {
		return fmt.Errorf("'key' parameter is required for key action")
	}
	return b.KeyboardPress(key)
}

func doScroll(b ComputerBackend, params map[string]any) error {
	dirStr, ok := params["scroll_direction"].(string)
	if !ok {
		return fmt.Errorf("'scroll_direction' parameter is required for scroll action")
	}
	dir, err := parseScrollDir(dirStr)
	if err != nil {
		return err
	}
	amount := extractOptionalInt(params, "amount")
	return b.Scroll(dir, amount, nil)
}

func doWait(params map[string]any) error {
	ms := extractOptionalInt(params, "milliseconds")
	if ms <= 0 {
		return fmt.Errorf("'milliseconds' parameter is required for wait action")
	}
	if ms > maxWaitMs {
		return fmt.Errorf("wait time exceeds maximum of %d ms", maxWaitMs)
	}
	// Note: TranslateAnthropicAction has no context parameter.
	// The handler-level "wait" tool (waitHandler.Execute) supports context.
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractCoordinate(params map[string]any, key string) (*Point, error) {
	val, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("'%s' parameter is required", key)
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("'%s' must be a [x, y] array", key)
	}
	if len(arr) < 2 {
		return nil, fmt.Errorf("'%s' must have at least 2 elements", key)
	}
	x, err := toInt(arr[0])
	if err != nil {
		return nil, fmt.Errorf("'%s[0]' must be an integer", key)
	}
	y, err := toInt(arr[1])
	if err != nil {
		return nil, fmt.Errorf("'%s[1]' must be an integer", key)
	}
	return &Point{X: x, Y: y}, nil
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case float64:
		return int(n), nil
	case int64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}
