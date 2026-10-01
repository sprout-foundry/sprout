package computer_use

import (
	"context"
	"fmt"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// handlers_keyboard.go — the keyboard computer-use tool handlers
// (keyboardType, keyboardPress), split out of handlers.go.
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

func (h *keyboardTypeHandler) Aliases() []string { return nil }

func (h *keyboardTypeHandler) Timeout() time.Duration { return 0 }

func (h *keyboardTypeHandler) MaxResultSize() int { return 0 }

func (h *keyboardTypeHandler) SafeForParallel() bool { return false }

func (h *keyboardTypeHandler) Interactive() bool { return false }

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

func (h *keyboardPressHandler) Aliases() []string { return nil }

func (h *keyboardPressHandler) Timeout() time.Duration { return 0 }

func (h *keyboardPressHandler) MaxResultSize() int { return 0 }

func (h *keyboardPressHandler) SafeForParallel() bool { return false }

func (h *keyboardPressHandler) Interactive() bool { return false }

// ---------------------------------------------------------------------------
// scroll
// ---------------------------------------------------------------------------
