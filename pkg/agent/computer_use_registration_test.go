package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/configuration"
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

// newComputerUseTestAgent builds a minimal Agent wired to an isolated
// config manager with computer_use enabled. Callers mutate the returned
// config (e.g. setting SkipPrompt) via the Manager before asserting on
// checkComputerUseActivation(). The agent is a top-level (non-subagent)
// instance with no API client.
func newComputerUseTestAgent(t *testing.T) *Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ComputerUse = &configuration.ComputerUseConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("enable computer_use: %v", err)
	}
	a := NewTestAgent()
	a.configManager = mgr
	return a
}

// TestCheckComputerUseActivation_BlocksSkipPrompt verifies that the
// --skip-prompt / daemon-mode gate rejects activation. cfg.SkipPrompt is
// set by both the --skip-prompt CLI flag and the daemon's direct mode
// (cmd/agent_modes.go), so this single check covers both non-interactive
// paths. The error must mention both conditions so the user understands.
func TestCheckComputerUseActivation_BlocksSkipPrompt(t *testing.T) {
	a := newComputerUseTestAgent(t)
	if err := a.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("set SkipPrompt: %v", err)
	}

	err := a.checkComputerUseActivation()
	if err == nil {
		t.Fatal("expected activation to be blocked when SkipPrompt is true, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--skip-prompt") {
		t.Errorf("error should mention --skip-prompt, got: %s", msg)
	}
	if !strings.Contains(msg, "daemon") {
		t.Errorf("error should mention daemon mode, got: %s", msg)
	}
}

// TestCheckComputerUseActivation_AllowsInteractiveWithOtherGatesOK verifies
// that an interactive top-level agent (SkipPrompt == false) on a supported
// platform with no vision-incompatible client passes all gates. The platform
// check is environment-dependent (needs cliclick/xdotool), so the test skips
// when the host can't actually run computer use.
func TestCheckComputerUseActivation_AllowsInteractiveWithOtherGatesOK(t *testing.T) {
	a := newComputerUseTestAgent(t)
	// SkipPrompt defaults to false in an isolated config; leave it that way.
	// No API client is set, so the vision gate (a.client != nil) is skipped.

	// The platform-support gate depends on the host having cliclick (macOS) or
	// xdotool+scrot (Linux). Skip gracefully in environments without them so
	// this test does not spuriously fail in CI.
	if support := computer_use.CheckPlatformSupport(); !support.Supported {
		t.Skipf("host does not support computer use: %s", support.Reason)
	}

	if err := a.checkComputerUseActivation(); err != nil {
		t.Errorf("interactive activation with all gates passing should succeed, got: %v", err)
	}
}
