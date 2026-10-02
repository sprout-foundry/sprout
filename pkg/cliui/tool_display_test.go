package cliui

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/console"
)

func TestFormatToolEndLine_SingleSpaceAfterGlyph(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	shell := FormatToolEndLine(0, "", console.GlyphShell.Prefix(), "shell_command", " (ls -la)", 0.2)
	if !strings.Contains(shell, console.GlyphShell.Rune()+" ls -la") {
		t.Errorf("shell end line = %q, want one space between glyph and command", shell)
	}
	tool := FormatToolEndLine(0, "", console.GlyphAction.Prefix(), "repo_map", "", 0.3)
	if !strings.Contains(tool, console.GlyphAction.Rune()+" repo_map") {
		t.Errorf("tool end line = %q, want one space between glyph and tool", tool)
	}
}

func TestToolRunLine_FirstArgUnwrappedLikeTheRest(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	run := &ToolRunState{Name: "list_directory", Count: 1}
	for _, preview := range []string{" (cmd)", " (pkg)", " (roadmap)"} {
		run.AppendArg(preview)
	}
	line := FormatToolRunLine(0, "", console.GlyphAction.Prefix(), run.Name, 3, run.ArgsTrail, 0.1)
	if !strings.Contains(line, "× 3 (cmd, pkg, roadmap)") {
		t.Errorf("run line = %q, want the args trail as one clean list", line)
	}
}
