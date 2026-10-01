package computer_use

import (
	"context"
	"fmt"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// handlers_scroll.go — the scroll and wait computer-use tool
// handlers, split out of handlers.go.
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

func (h *scrollHandler) Aliases() []string { return nil }

func (h *scrollHandler) Timeout() time.Duration { return 0 }

func (h *scrollHandler) MaxResultSize() int { return 0 }

func (h *scrollHandler) SafeForParallel() bool { return false }

func (h *scrollHandler) Interactive() bool { return false }

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

func (h *waitHandler) Aliases() []string { return nil }

func (h *waitHandler) Timeout() time.Duration { return 0 }

func (h *waitHandler) MaxResultSize() int { return 0 }

func (h *waitHandler) SafeForParallel() bool { return false }

func (h *waitHandler) Interactive() bool { return false }
