package agent

import "testing"

// SP-048 follow-up: tools that render their own UI (ask_user) must declare
// Interactive=true so CLI subscribers can suppress spinner chrome that
// would otherwise overwrite the tool's prompt.

func TestToolRegistry_IsInteractive_AskUserFlagged(t *testing.T) {
	r := GetToolRegistry()
	if !r.IsInteractive("ask_user") {
		t.Errorf("ask_user should be registered with Interactive=true")
	}
}

// shell_command also owns the terminal during execution (streams
// subprocess output via io.MultiWriter). The activity-indicator spinner
// would interleave with that stream and produce the cursor-thrash bug we
// hit in real interactive sessions.
func TestToolRegistry_IsInteractive_ShellCommandFlagged(t *testing.T) {
	r := GetToolRegistry()
	if !r.IsInteractive("shell_command") {
		t.Errorf("shell_command should be registered with Interactive=true (streams live stdout)")
	}
}

func TestToolRegistry_IsInteractive_NonInteractiveToolsReturnFalse(t *testing.T) {
	r := GetToolRegistry()
	// Sample of tools that definitely should NOT be interactive — they
	// return a result to the agent without owning the terminal.
	for _, name := range []string{"read_file", "TodoRead", "search_files"} {
		if r.IsInteractive(name) {
			t.Errorf("%s should not be Interactive", name)
		}
	}
}

func TestToolRegistry_IsInteractive_UnknownReturnsFalse(t *testing.T) {
	r := GetToolRegistry()
	if r.IsInteractive("definitely_not_a_real_tool") {
		t.Errorf("unknown tools should not be Interactive")
	}
	if r.IsInteractive("") {
		t.Errorf("empty name should not be Interactive")
	}
}

func TestIsInteractiveTool_TopLevelHelper(t *testing.T) {
	if !IsInteractiveTool("ask_user") {
		t.Errorf("top-level IsInteractiveTool should agree with the registry")
	}
	if IsInteractiveTool("read_file") {
		t.Errorf("top-level IsInteractiveTool should agree with the registry")
	}
}
