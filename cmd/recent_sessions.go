//go:build !js

package cmd

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// recentSessionsWindow is how far back we look when surfacing the
// recent-sessions list on startup. Anything older is hidden as noise.
const recentSessionsWindow = 7 * 24 * time.Hour

// maxRecentSessionsShown caps the list so even chatty workspaces don't
// drown the welcome screen.
const maxRecentSessionsShown = 3

// maybeShowRecentSessions prints up to maxRecentSessionsShown recent
// sessions for the current workspace if any exist within the
// recentSessionsWindow lookback. SP-048-5a.
//
// We surface the continuation command rather than implementing inline
// "press 1 to resume" because resuming a session requires re-initializing
// the agent before its first query — clean to do via the existing
// `--session-id` flag path at startup, awkward to splice in mid-process.
// Users get a copy-pasteable command and can rerun sprout.
//
// Silent on:
//   - errors enumerating sessions (best-effort feature)
//   - empty result (don't clutter on first run)
//   - current session being the only/most-recent entry (no useful context)
//
// Output goes to stderr so piped stdout stays clean.
func maybeShowRecentSessions(chatAgent *agent.Agent) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	sessions, err := agent.ListSessionsWithTimestampsScoped(cwd)
	if err != nil || len(sessions) == 0 {
		return
	}

	cutoff := time.Now().Add(-recentSessionsWindow)
	current := ""
	if chatAgent != nil {
		current = chatAgent.GetSessionID()
	}

	recent := make([]agent.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.LastUpdated.Before(cutoff) {
			continue
		}
		// Don't show the session we just created.
		if current != "" && s.SessionID == current {
			continue
		}
		recent = append(recent, s)
	}
	if len(recent) == 0 {
		return
	}

	// Already sorted by LastUpdated descending from
	// ListSessionsWithTimestampsScoped, but guard against future drift.
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].LastUpdated.After(recent[j].LastUpdated)
	})

	if len(recent) > maxRecentSessionsShown {
		recent = recent[:maxRecentSessionsShown]
	}

	fmt.Fprintln(os.Stderr, "[chart] Recent sessions in this workspace:")
	for _, s := range recent {
		label := s.Name
		if label == "" {
			label = s.SessionID
		}
		fmt.Fprintf(os.Stderr, "  %-7s  %s  %s\n",
			humanizeAge(time.Since(s.LastUpdated)),
			s.SessionID,
			truncateLabel(label, 48),
		)
	}
	fmt.Fprintln(os.Stderr, "  Resume any with: sprout agent --session-id <id>")
	fmt.Fprintln(os.Stderr)
}

// humanizeAge renders a duration as a short, friendly relative time
// suitable for a single-column listing ("2h ago", "3d ago", "just now").
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

// truncateLabel keeps the recent-sessions table aligned even when a
// session name (or fallback ID) is verbose.
func truncateLabel(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
