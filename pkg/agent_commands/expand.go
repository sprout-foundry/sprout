package commands

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ExpandCommand implements /expand — print the full result of a recent
// tool call that the transcript reduced to a one-line summary (SP-171d).
type ExpandCommand struct {
	outputSink
}

// Name returns the command name.
func (e *ExpandCommand) Name() string {
	return "expand"
}

// Description returns the command description.
func (e *ExpandCommand) Description() string {
	return "Print the full output of a recent tool call"
}

// Usage returns the detailed help text shown by `/help expand`.
func (e *ExpandCommand) Usage() string {
	return strings.Join([]string{
		"/expand [number]   Print the full output of a recent tool call.",
		"",
		"Without a number, prints the most recent one. With no argument at",
		"all and nothing buffered, lists what is available.",
		"",
		"The CLI keeps the last " + fmt.Sprint(cliui.ToolOutputRingSize) + " completed tool results. Older",
		"results are still in your terminal's own scrollback.",
	}, "\n")
}

// SafeDuringSteer reports that /expand may run while a turn is in flight:
// it only reads the tool-output ring and writes to the transcript.
func (e *ExpandCommand) SafeDuringSteer() bool { return true }

// Execute prints the requested tool output.
func (e *ExpandCommand) Execute(args []string, _ *agent.Agent) error {
	entries := cliui.RecentToolOutputs()

	// `/expand` with no argument on a non-empty buffer expands the most
	// recent call; `/expand list` (or any non-numeric argument) lists.
	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	if query == "list" || query == "ls" {
		e.println(cliui.FormatToolOutputList(entries))
		return nil
	}

	entry, ok := cliui.FindToolOutput(entries, query)
	if !ok {
		if query == "" {
			e.println(cliui.FormatToolOutputList(entries))
			return nil
		}
		return fmt.Errorf("no tool output numbered %q in the last %d calls — /expand list shows what is available", query, cliui.ToolOutputRingSize)
	}

	header := fmt.Sprintf("%s %s", entry.ToolName, entry.Status)
	if entry.Preview != "" {
		header = fmt.Sprintf("%s(%s) %s", entry.ToolName, entry.Preview, entry.Status)
	}
	e.println(console.GlyphInfo.Prefix() + " " + header)
	if entry.Output == "" {
		e.println("  (the tool returned no output)")
		return nil
	}
	e.print(entry.Output)
	if !strings.HasSuffix(entry.Output, "\n") {
		e.println()
	}
	return nil
}
