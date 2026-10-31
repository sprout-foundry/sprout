package cliui

// tool_output_ring.go — the expandable tool-output buffer behind
// `/expand` (SP-171d).
//
// The CLI prints a one-line summary per tool call and the full result is
// gone once it scrolls. Best-in-class CLIs keep the last N results
// addressable so a user can ask for one back without re-running the tool.
// A fixed-size ring of the N most recent completed calls is enough: older
// results are still in the terminal's own scrollback, and an unbounded
// buffer would hold every large read in memory for the whole session.

import (
	"fmt"
	"strings"
	"sync"
)

// ToolOutputRingSize is how many completed tool calls stay addressable
// via /expand. Chosen to cover a typical turn's worth of calls.
const ToolOutputRingSize = 10

// ToolOutput is one completed tool call: what ran, how it ended, and the
// result text the CLI did not print.
type ToolOutput struct {
	Seq      int    // 1-based, monotonic across the session
	ToolName string // e.g. "read_file"
	Preview  string // the same argument preview the summary line showed
	Status   string // "completed", "error", ...
	Output   string // full result text
}

// toolOutputRing is a fixed-capacity ring of completed tool calls.
type toolOutputRing struct {
	mu      sync.Mutex
	entries []ToolOutput
	nextSeq int
}

var sessionToolOutputs = &toolOutputRing{}

// RecordToolOutput stores one completed call's result. Empty output is
// still recorded: "the tool returned nothing" is a useful thing to
// expand, and skipping it would make the numbering skip.
func RecordToolOutput(toolName, preview, status, output string) {
	sessionToolOutputs.record(toolName, preview, status, output)
}

func (r *toolOutputRing) record(toolName, preview, status, output string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	entry := ToolOutput{
		Seq:      r.nextSeq,
		ToolName: toolName,
		Preview:  preview,
		Status:   status,
		Output:   output,
	}
	if len(r.entries) < ToolOutputRingSize {
		r.entries = append(r.entries, entry)
		return
	}
	copy(r.entries, r.entries[1:])
	r.entries[len(r.entries)-1] = entry
}

// RecentToolOutputs returns the buffered calls, oldest first.
func RecentToolOutputs() []ToolOutput {
	return sessionToolOutputs.snapshot()
}

func (r *toolOutputRing) snapshot() []ToolOutput {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolOutput, len(r.entries))
	copy(out, r.entries)
	return out
}

// ResetToolOutputs clears the buffer. Tests use it for isolation; the
// REPL never calls it (a session's history is the session's history).
func ResetToolOutputs() {
	sessionToolOutputs.mu.Lock()
	defer sessionToolOutputs.mu.Unlock()
	sessionToolOutputs.entries = nil
	sessionToolOutputs.nextSeq = 0
}

// FormatToolOutputList renders the /expand index: one line per buffered
// call, newest last, each with the sequence number the user types back.
func FormatToolOutputList(entries []ToolOutput) string {
	if len(entries) == 0 {
		return "No tool output buffered yet — results appear here once a tool has run."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recent tool output (last %d):\n", len(entries))
	for _, e := range entries {
		line := fmt.Sprintf("  %d  %s", e.Seq, e.ToolName)
		if e.Preview != "" {
			line += "(" + e.Preview + ")"
		}
		if e.Status != "" && e.Status != "completed" {
			line += "  [" + e.Status + "]"
		}
		if e.Output == "" {
			line += "  (no output)"
		} else {
			line += fmt.Sprintf("  · %s", FormatResultSize(len(e.Output)))
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("Type /expand <number> to print one.")
	return b.String()
}

// FindToolOutput resolves a /expand argument to a buffered call. An empty
// query selects the most recent entry.
func FindToolOutput(entries []ToolOutput, query string) (ToolOutput, bool) {
	if len(entries) == 0 {
		return ToolOutput{}, false
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return entries[len(entries)-1], true
	}
	var seq int
	if _, err := fmt.Sscanf(query, "%d", &seq); err != nil {
		return ToolOutput{}, false
	}
	for _, e := range entries {
		if e.Seq == seq {
			return e, true
		}
	}
	return ToolOutput{}, false
}
