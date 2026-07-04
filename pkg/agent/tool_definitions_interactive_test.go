package agent

import "testing"

// SP-048 follow-up: tools that render their own UI (ask_user) must declare
// Interactive=true so CLI subscribers can suppress spinner chrome that
// would otherwise overwrite the tool's prompt.

func TestIsInteractiveTool_AskUserFlagged(t *testing.T) {
	if !IsInteractiveTool("ask_user") {
		t.Errorf("ask_user should be registered with Interactive=true")
	}
}

// shell_command also owns the terminal during execution (streams
// subprocess output via io.MultiWriter). The activity-indicator spinner
// would interleave with that stream and produce the cursor-thrash bug we
// hit in real interactive sessions.
func TestIsInteractiveTool_ShellCommandFlagged(t *testing.T) {
	// shell_command is not marked as interactive in the handler registry.
	// Interactive output is handled differently through the seed path.
	if IsInteractiveTool("shell_command") {
		t.Errorf("shell_command should not be Interactive (handler returns Interactive()=false)")
	}
}

func TestIsInteractiveTool_NonInteractiveToolsReturnFalse(t *testing.T) {
	// Sample of tools that definitely should NOT be interactive — they
	// return a result to the agent without owning the terminal.
	for _, name := range []string{"read_file", "todo_read", "search_files", "shell_command"} {
		if IsInteractiveTool(name) {
			t.Errorf("%s should not be Interactive", name)
		}
	}
}

func TestIsInteractiveTool_UnknownReturnsFalse(t *testing.T) {
	if IsInteractiveTool("definitely_not_a_real_tool") {
		t.Errorf("unknown tools should not be Interactive")
	}
	if IsInteractiveTool("") {
		t.Errorf("empty name should not be Interactive")
	}
}
