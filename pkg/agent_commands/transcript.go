package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TranscriptCommand implements the /transcript slash command, a
// diagnostic tool for inspecting the agent's current conversation
// state. The default invocation captures a JSON snapshot to
// ~/.sprout/transcripts/<scope>/<session>/<utc>-manual.json. Subcommands
// enrich or change the output:
//
//	/transcript                # snapshot only
//	/transcript preview        # snapshot + would-be /compact preview
//	/transcript markdown       # snapshot + rendered .md alongside
//	/transcript diff           # snapshot + diff vs previous snapshot
//
// Multiple subcommands can be combined (e.g. `/transcript preview diff`).
// All snapshots include per-message source annotations so a reader can
// see which messages are LLM-generated checkpoint substitutes vs the
// original turns.
type TranscriptCommand struct{}

// Name returns the command name
func (c *TranscriptCommand) Name() string {
	return "transcript"
}

// Description returns the command description
func (c *TranscriptCommand) Description() string {
	return "Capture a JSON snapshot of the current conversation for inspection (subcommands: preview, markdown, diff)"
}

// Usage returns the detailed help text shown by `/help transcript`.
func (c *TranscriptCommand) Usage() string {
	return strings.TrimSpace(`
/transcript                 Snapshot the current conversation to JSON.
/transcript preview         Also include what /compact would substitute right now.
/transcript markdown        Also write a human-readable .md alongside the JSON.
/transcript diff            Also diff against the previous snapshot for this session.

Snapshots land in ~/.sprout/transcripts/<scope>/<session>/<utc>-<label>.json.
Each captures the live message list, current turn checkpoints, token/cost
counters, and per-message source annotations (original / LLM-checkpoint).
`)
}

// Execute runs the transcript command.
func (c *TranscriptCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	wantPreview := false
	wantMarkdown := false
	wantDiff := false
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "preview":
			wantPreview = true
		case "markdown", "md":
			wantMarkdown = true
		case "diff":
			wantDiff = true
		case "":
			continue
		default:
			return fmt.Errorf("unknown /transcript subcommand: %q (valid: preview, markdown, diff)", a)
		}
	}

	// Locate prior snapshot before writing the new one so the diff
	// path can compare new-vs-previous correctly.
	var priorPath string
	if wantDiff {
		sessionID := chatAgent.GetSessionID()
		workingDir, _ := os.Getwd()
		paths, err := agent.ListTranscriptSnapshots(sessionID, workingDir)
		if err != nil {
			fmt.Printf("[transcript] could not list prior snapshots: %v\n", err)
		} else if len(paths) > 0 {
			priorPath = paths[len(paths)-1]
		}
	}

	label := "manual"
	if wantPreview {
		label = "manual-preview"
	}
	path, err := chatAgent.CaptureTranscriptSnapshot(label, wantPreview)
	if err != nil {
		return fmt.Errorf("failed to capture transcript snapshot: %w", err)
	}
	fmt.Printf("\n[transcript] snapshot: %s\n", path)

	if wantMarkdown {
		mdPath, mdErr := writeTranscriptMarkdown(path)
		if mdErr != nil {
			fmt.Printf("[transcript] markdown render failed: %v\n", mdErr)
		} else {
			fmt.Printf("[transcript] markdown: %s\n", mdPath)
		}
	}

	if wantDiff {
		if priorPath == "" {
			fmt.Println("[transcript] no prior snapshot for this session to diff against")
		} else {
			if err := printTranscriptDiff(priorPath, path); err != nil {
				fmt.Printf("[transcript] diff failed: %v\n", err)
			}
		}
	}

	return nil
}

// writeTranscriptMarkdown renders a snapshot to a .md file living
// alongside the JSON. Returns the path written.
func writeTranscriptMarkdown(jsonPath string) (string, error) {
	snap, err := agent.LoadTranscriptSnapshot(jsonPath)
	if err != nil {
		return "", err
	}
	mdPath := strings.TrimSuffix(jsonPath, ".json") + ".md"
	rendered := renderSnapshotAsMarkdown(snap)
	if err := os.WriteFile(mdPath, []byte(rendered), 0600); err != nil {
		return "", fmt.Errorf("failed to write markdown: %w", err)
	}
	return mdPath, nil
}

func renderSnapshotAsMarkdown(snap *agent.TranscriptSnapshot) string {
	if snap == nil || snap.State == nil {
		return "# Transcript snapshot\n\n(no state)\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Transcript snapshot — %s\n\n", snap.Label)
	fmt.Fprintf(&b, "- **Session**: `%s`\n", snap.SessionID)
	fmt.Fprintf(&b, "- **Working dir**: `%s`\n", snap.WorkingDirectory)
	fmt.Fprintf(&b, "- **Captured**: %s\n", snap.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&b, "- **Messages**: %d\n", len(snap.State.Messages))
	fmt.Fprintf(&b, "- **Turn checkpoints**: %d\n", len(snap.State.TurnCheckpoints))
	fmt.Fprintf(&b, "- **Total tokens**: %d\n", snap.State.TotalTokens)
	fmt.Fprintf(&b, "- **Total cost**: $%.6f\n\n", snap.State.TotalCost)

	if snap.CompactPreview != nil {
		fmt.Fprintf(&b, "## /compact preview\n\n")
		fmt.Fprintf(&b, "- Before: %d messages\n", snap.CompactPreview.BeforeMessageCount)
		fmt.Fprintf(&b, "- After:  %d messages\n", snap.CompactPreview.AfterMessageCount)
		fmt.Fprintf(&b, "- Would reduce: %v\n\n", snap.CompactPreview.WouldReduce)
	}

	if len(snap.FileChanges) > 0 {
		fmt.Fprintf(&b, "## File changes\n\n")
		if snap.ChangeTrackerRev != "" {
			fmt.Fprintf(&b, "- Change-tracker revision: `%s`\n\n", snap.ChangeTrackerRev)
		}
		for _, c := range snap.FileChanges {
			attr := c.Source
			if c.ToolCall != "" {
				attr += ": " + c.ToolCall
			}
			fmt.Fprintf(&b, "- **%s** `%s` (%s)\n", c.Operation, c.Path, attr)
		}
		fmt.Fprintln(&b)
	}

	if len(snap.State.TurnCheckpoints) > 0 {
		fmt.Fprintf(&b, "## Turn checkpoints\n\n")
		for i, cp := range snap.State.TurnCheckpoints {
			fmt.Fprintf(&b, "### Checkpoint %d — messages [%d..%d]\n\n", i+1, cp.StartIndex, cp.EndIndex)
			if cp.ActionableSummary != "" {
				fmt.Fprintf(&b, "**Actionable summary:**\n\n```\n%s\n```\n\n", cp.ActionableSummary)
			}
			if cp.Summary != "" {
				fmt.Fprintf(&b, "**Summary:**\n\n```\n%s\n```\n\n", cp.Summary)
			}
		}
	}

	fmt.Fprintf(&b, "## Messages\n\n")
	for i, msg := range snap.State.Messages {
		source := "original"
		if i < len(snap.MessageAnnotations) {
			source = string(snap.MessageAnnotations[i].Source)
		}
		fmt.Fprintf(&b, "### [%d] %s — source=%s, %d chars", i, msg.Role, source, len(msg.Content))
		if len(msg.ToolCalls) > 0 {
			fmt.Fprintf(&b, ", %d tool_calls", len(msg.ToolCalls))
		}
		fmt.Fprintf(&b, "\n\n")
		if strings.TrimSpace(msg.Content) != "" {
			fmt.Fprintf(&b, "```\n%s\n```\n\n", msg.Content)
		}
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(&b, "- tool_call: `%s` args=`%s`\n", tc.Function.Name, tc.Function.Arguments)
		}
		if len(msg.ToolCalls) > 0 {
			fmt.Fprintln(&b)
		}
	}
	return b.String()
}

func printTranscriptDiff(olderPath, newerPath string) error {
	older, err := agent.LoadTranscriptSnapshot(olderPath)
	if err != nil {
		return err
	}
	newer, err := agent.LoadTranscriptSnapshot(newerPath)
	if err != nil {
		return err
	}
	diff := agent.DiffTranscriptSnapshots(older, newer)
	if diff == nil {
		fmt.Println("[transcript] diff produced no result (one snapshot was empty)")
		return nil
	}
	fmt.Println("\n[transcript] diff vs previous snapshot:")
	fmt.Printf("       Previous: %s (%d msgs, %d checkpoints, %d tokens)\n",
		filepath.Base(olderPath), diff.OlderMessageCount, diff.OlderCheckpointCount, diff.OlderTotalTokens)
	fmt.Printf("       Current:  %s (%d msgs, %d checkpoints, %d tokens)\n",
		filepath.Base(newerPath), diff.NewerMessageCount, diff.NewerCheckpointCount, diff.NewerTotalTokens)
	if diff.MessagesDroppedAtTail > 0 {
		fmt.Printf("       Dropped at tail: %d messages\n", diff.MessagesDroppedAtTail)
	}
	if len(diff.ChangedIndices) > 0 {
		fmt.Printf("       Replaced in place: %d messages\n", len(diff.ChangedIndices))
		for _, entry := range diff.ChangedIndices {
			fmt.Printf("         [%d] %s/%s → %s/%s\n", entry.Index, entry.OlderRole, entry.OlderSource, entry.NewerRole, entry.NewerSource)
		}
	}
	if len(diff.NewFileChanges) > 0 {
		fmt.Printf("       New file changes (%d):\n", len(diff.NewFileChanges))
		for _, c := range diff.NewFileChanges {
			attr := c.Source
			if c.ToolCall != "" {
				attr += ":" + c.ToolCall
			}
			fmt.Printf("         %s %s (%s)\n", c.Operation, c.Path, attr)
		}
	} else if diff.OlderFileChangeCount != diff.NewerFileChangeCount {
		fmt.Printf("       File changes: %d → %d (no new entries; existing rolled forward)\n",
			diff.OlderFileChangeCount, diff.NewerFileChangeCount)
	}
	for _, note := range diff.Notes {
		fmt.Printf("       Note: %s\n", note)
	}
	// Marshal the structured diff alongside the new snapshot for
	// programmatic access.
	diffPath := strings.TrimSuffix(newerPath, ".json") + ".diff.json"
	if data, err := json.MarshalIndent(diff, "", "  "); err == nil {
		if err := os.WriteFile(diffPath, data, 0600); err == nil {
			fmt.Printf("       Diff JSON: %s\n", diffPath)
		}
	}
	return nil
}
