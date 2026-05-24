//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"golang.org/x/term"
)

// recentSessionsWindow is how far back we look when surfacing the
// recent-sessions list on startup. Anything older is hidden as noise.
const recentSessionsWindow = 7 * 24 * time.Hour

// maxRecentSessionsShown caps the list so even chatty workspaces don't
// drown the welcome screen.
const maxRecentSessionsShown = 3

// maybeOfferSessionResume prints up to maxRecentSessionsShown recent
// sessions for the current workspace (last recentSessionsWindow), then
// offers inline numeric selection. If the user picks a valid number, the
// corresponding session is loaded into chatAgent (state + session ID)
// and the user proceeds in that session. SP-048-5a.
//
// Behavior:
//   - Empty result: silent, no prompt, no state change.
//   - Non-TTY stdin (piped input): list is still printed for visibility,
//     but no selection prompt is shown — the agent starts a fresh session.
//   - Invalid number / non-numeric / blank input: starts a fresh session.
//   - Valid number: LoadStateScoped + ApplyState + SetSessionID inline.
//
// Best-effort throughout: any failure during state load is surfaced as a
// [FAIL] line but the function returns so the user gets a working REPL
// instead of a wedged startup.
//
// Up/down arrows are NOT used for selection — those stay reserved for
// command history. Selection is by number, intentionally simple.
func maybeOfferSessionResume(chatAgent *agent.Agent) {
	// If the user explicitly chose a session via flag, the picker is
	// redundant — and worse, picking a different number would stomp
	// the state we just loaded. Same applies when the agent already
	// has a conversation loaded (covers the workflow restore path
	// where state arrives via Orchestration.ConversationSessionID).
	if strings.TrimSpace(agentSessionID) != "" || agentLastSession {
		return
	}
	if chatAgent != nil && len(chatAgent.GetMessages()) > 0 {
		return
	}

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
		// Don't offer the session we just created (it's empty).
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
	for i, s := range recent {
		label := s.Name
		if label == "" {
			label = s.SessionID
		}
		fmt.Fprintf(os.Stderr, "  %d) %-9s  %s\n",
			i+1,
			humanizeAge(time.Since(s.LastUpdated)),
			truncateLabel(label, 56),
		)
	}

	// Skip the interactive prompt when stdin isn't a TTY (e.g. `sprout < script`).
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr)
		return
	}

	fmt.Fprintf(os.Stderr, "  Resume one? [1-%d or Enter to start fresh]: ", len(recent))

	reader := bufio.NewReader(os.Stdin)
	raw, readErr := reader.ReadString('\n')
	if readErr != nil {
		// Stdin closed / interrupted — proceed with fresh session.
		fmt.Fprintln(os.Stderr)
		return
	}
	choice := strings.TrimSpace(raw)
	if choice == "" {
		return
	}
	idx, parseErr := strconv.Atoi(choice)
	if parseErr != nil || idx < 1 || idx > len(recent) {
		fmt.Fprintf(os.Stderr, "  [skip] %q is not a valid choice, starting fresh\n", choice)
		return
	}

	chosen := recent[idx-1]
	state, err := chatAgent.LoadStateScoped(chosen.SessionID, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  [FAIL] could not load session %s: %v\n", chosen.SessionID, err)
		return
	}
	chatAgent.ApplyState(state)
	chatAgent.SetSessionID(state.SessionID)

	label := chosen.Name
	if label == "" {
		label = chosen.SessionID
	}
	fmt.Fprintf(os.Stderr, "[OK] Resumed: %s\n", truncateLabel(label, 64))
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
