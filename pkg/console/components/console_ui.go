package components

import (
    "context"
    "fmt"
    "os"
    "time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
	"golang.org/x/term"
)

// ConsoleUI provides UI capabilities to the agent via the console
type ConsoleUI struct {
	agentConsole  *AgentConsole
	consoleApp    *ConsoleApp
	isInteractive bool
}

// NewConsoleUI creates a new ConsoleUI
func NewConsoleUI(agentConsole *AgentConsole) *ConsoleUI {
	// Check if we're in an interactive terminal
	isInteractive := term.IsTerminal(int(os.Stdin.Fd())) &&
		os.Getenv("CI") == "" &&
		os.Getenv("GITHUB_ACTIONS") == ""

	var consoleApp *ConsoleApp
	if isInteractive && agentConsole != nil && agentConsole.Deps.Terminal != nil {
		consoleApp = NewConsoleApp(agentConsole.Deps.Terminal)
	}

	return &ConsoleUI{
		agentConsole:  agentConsole,
		consoleApp:    consoleApp,
		isInteractive: isInteractive,
	}
}

// ShowDropdown implements agent.UI
func (c *ConsoleUI) ShowDropdown(ctx context.Context, items []ui.DropdownItem, options ui.DropdownOptions) (ui.DropdownItem, error) {
	if !c.isInteractive {
		return nil, ui.ErrUINotAvailable
	}

	// Use the new component-based dropdown if available
    if c.consoleApp != nil {
        // Put input manager in passthrough mode to avoid interference
        if c.agentConsole != nil && c.agentConsole.inputManager != nil {
            c.agentConsole.inputManager.SetPassthroughMode(true)
            // Give InputManager time to fully stop and release stdin to the dropdown
            time.Sleep(100 * time.Millisecond)
            // Ensure layout is fully restored after dropdown completes
            defer func() {
                c.agentConsole.inputManager.SetPassthroughMode(false)
                // Restore scroll region, footer and cursor positioning
                c.agentConsole.restoreLayoutAfterPassthrough()
            }()
        }

		// Convert dropdown items to the format expected by the new UI
		convertedItems := make([]interface{}, len(items))
		for i, item := range items {
			convertedItems[i] = item
		}

		// Create options map
		opts := map[string]interface{}{
			"prompt":       options.Prompt,
			"searchPrompt": options.SearchPrompt,
			"showCounts":   options.ShowCounts,
		}

		// Clear current line and show dropdown
		fmt.Print("\r\033[K") // Clear line

		// Show the new dropdown
		selected, err := c.consoleApp.ShowDropdown(ctx, convertedItems, opts)
		if err != nil {
			return nil, err
		}

		// Clear dropdown remnants
		fmt.Print("\r\033[K")

		// Convert back to DropdownItem
		if dropdownItem, ok := selected.(ui.DropdownItem); ok {
			return dropdownItem, nil
		}

		return nil, fmt.Errorf("unexpected dropdown result type")
	}

    // No component app available; UI not available in this context
    return nil, ui.ErrUINotAvailable
}

// ShowQuickPrompt renders a minimal inline prompt above the input and captures a quick choice
func (c *ConsoleUI) ShowQuickPrompt(ctx context.Context, prompt string, options []ui.QuickOption, horizontal bool) (ui.QuickOption, error) {
    if !c.isInteractive || c.agentConsole == nil || c.agentConsole.inputManager == nil {
        return ui.QuickOption{}, ui.ErrUINotAvailable
    }

    // Enter passthrough mode to capture direct key input
    c.agentConsole.inputManager.SetPassthroughMode(true)
    defer func() {
        c.agentConsole.inputManager.SetPassthroughMode(false)
        c.agentConsole.restoreLayoutAfterPassthrough()
    }()

    // Determine line above input
    y := c.agentConsole.inputManager.GetCurrentInputFieldLine() - 1
    if y < 1 {
        y = 1
    }

    // Build prompt line
    line := BuildQuickPromptLine(prompt, options, horizontal)

    // Render prompt
    c.agentConsole.Terminal().MoveCursor(1, y)
    c.agentConsole.Terminal().ClearLine()
    c.agentConsole.Terminal().WriteText(line)

    // Simple key mapping: digits 1..n or first-letter hotkeys (case-insensitive), Enter=first, Esc=cancel
    keyToIndex := func(b byte) (int, bool) {
        if b >= '1' && b <= '9' {
            idx := int(b - '1')
            if idx >= 0 && idx < len(options) { return idx, true }
        }
        // letters
        lower := b
        if b >= 'A' && b <= 'Z' { lower = b + 32 }
        for i, opt := range options {
            hk := opt.Hotkey
            if hk == 0 && len(opt.Label) > 0 { hk = rune([]rune(opt.Label)[0]) }
            if hk >= 'A' && hk <= 'Z' { hk = hk + 32 }
            if rune(lower) == hk { return i, true }
        }
        return 0, false
    }

    // Read single key with context cancellation support (rudimentary)
    var buf [1]byte
    for {
        select {
        case <-ctx.Done():
            return ui.QuickOption{}, ui.ErrCancelled
        default:
            n, _ := os.Stdin.Read(buf[:])
            if n == 0 { continue }
            b := buf[0]
            if b == 27 { // ESC
                return ui.QuickOption{}, ui.ErrCancelled
            }
            if b == 13 || b == 10 { // Enter
                return options[0], nil
            }
            if idx, ok := keyToIndex(b); ok {
                return options[idx], nil
            }
        }
    }
}

// BuildQuickPromptLine constructs a simple horizontally-aligned quick prompt line
func BuildQuickPromptLine(prompt string, options []ui.QuickOption, horizontal bool) string {
    // Assign numeric hotkeys if none provided
    rendered := prompt
    if rendered != "" {
        rendered += "  "
    }
    for i, opt := range options {
        label := opt.Label
        hk := opt.Hotkey
        if hk == 0 {
            if i < 9 {
                hk = rune('1' + i)
            } else if len(label) > 0 {
                hk = []rune(label)[0]
            }
        }
        rendered += fmt.Sprintf("[%s] %s", string(hk), label)
        if i < len(options)-1 {
            rendered += "  "
            if !horizontal {
                rendered += "| "
            }
        }
    }
    return rendered
}

// IsInteractive implements agent.UI
func (c *ConsoleUI) IsInteractive() bool {
	return c.isInteractive
}

// convertToDropdownItems converts console dropdown items to UI dropdown items
func convertToDropdownItems(items []DropdownItem) []ui.DropdownItem {
	result := make([]ui.DropdownItem, len(items))
	for i, item := range items {
		result[i] = &dropdownItemAdapter{item: item}
	}
	return result
}

// dropdownItemAdapter adapts console.DropdownItem to ui.DropdownItem
type dropdownItemAdapter struct {
	item DropdownItem
}

func (d *dropdownItemAdapter) Display() string    { return d.item.Display() }
func (d *dropdownItemAdapter) SearchText() string { return d.item.SearchText() }
func (d *dropdownItemAdapter) Value() interface{} { return d.item.Value() }

// Cleanup cleans up UI resources
func (c *ConsoleUI) Cleanup() {
	if c.consoleApp != nil {
		c.consoleApp.Cleanup()
	}
}

// Setup UI for agent - call this after creating the agent console
func SetupAgentUI(agentConsole *AgentConsole, agent *agent.Agent) {
	ui := NewConsoleUI(agentConsole)
	agent.SetUI(ui)
}
