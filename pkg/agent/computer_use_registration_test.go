package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
)

func TestToolConfigFromHandler_MapsDefinition(t *testing.T) {
	handlers := computer_use.Handlers()
	if len(handlers) == 0 {
		t.Fatal("no computer_use handlers")
	}
	// take_screenshot is first and has a single optional "region" parameter.
	cfg := toolConfigFromHandler(handlers[0])
	if cfg.Name != "take_screenshot" {
		t.Errorf("name = %q, want take_screenshot", cfg.Name)
	}
	if cfg.Description == "" {
		t.Error("description should be populated")
	}
	if len(cfg.Parameters) != 1 || cfg.Parameters[0].Name != "region" {
		t.Errorf("parameters = %+v, want one 'region' param", cfg.Parameters)
	}
}

func TestIsComputerUseToolBlocked_InertWhenDisabled(t *testing.T) {
	// With computer use disabled, the restricted-name set is empty, so no tool
	// is ever blocked — the guard must be a no-op.
	saved := computerUseToolNames
	computerUseToolNames = map[string]bool{}
	defer func() { computerUseToolNames = saved }()

	if isComputerUseToolBlocked("mouse_click", nil) {
		t.Error("guard should be inert when the name set is empty")
	}
	if isComputerUseToolBlocked("read_file", nil) {
		t.Error("non computer-use tool must never be blocked")
	}
}

func TestIsComputerUseToolBlocked_BlocksWhenRegistered(t *testing.T) {
	saved := computerUseToolNames
	computerUseToolNames = map[string]bool{"mouse_click": true}
	defer func() { computerUseToolNames = saved }()

	// nil agent (no active computer_user persona) → blocked.
	if !isComputerUseToolBlocked("mouse_click", nil) {
		t.Error("computer-use tool should be blocked without the computer_user persona")
	}
	// A non-computer-use tool stays allowed.
	if isComputerUseToolBlocked("read_file", nil) {
		t.Error("unrelated tool must not be blocked")
	}
}

func TestRegisterComputerUseTools_NoopWhenDisabled(t *testing.T) {
	// nil config and disabled config must both be no-ops returning nil.
	if err := RegisterComputerUseTools(nil); err != nil {
		t.Errorf("nil config should be a no-op, got %v", err)
	}
}
