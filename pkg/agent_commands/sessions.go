package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SessionsCommand handles session management with auto-tracking and session recovery
type SessionsCommand struct{}

func (c *SessionsCommand) Name() string {
	return "sessions"
}

// SafeDuringSteer returns false - /sessions is session lifecycle, too risky
func (c *SessionsCommand) SafeDuringSteer() bool {
	return false
}

func (c *SessionsCommand) Description() string {
	return "Show and load previous conversation sessions"
}

// Usage returns the detailed help text shown by `/help sessions`.
func (c *SessionsCommand) Usage() string {
	return strings.Join([]string{
		"/sessions              Interactive picker: list and load a session.",
		"/sessions <number>     Load session by list number directly.",
		"",
		"Sessions are shown newest-first. Loading replaces the current",
		"conversation with the selected session's state.",
	}, "\n")
}

func (c *SessionsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// List sessions immediately in reverse order (newest first)
	workspaceRoot := ""
	if chatAgent != nil {
		workspaceRoot = chatAgent.GetWorkspaceRoot()
	}
	sessions, err := agent.ListSessionsWithTimestampsScoped(workspaceRoot)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No saved sessions found.")
		return nil
	}

	// If user provided a session number, load it directly
	if len(args) > 0 {
		sessionNum, err := strconv.Atoi(args[0])
		if err != nil || sessionNum < 1 || sessionNum > len(sessions) {
			return fmt.Errorf("invalid session number. Please select 1-%d", len(sessions))
		}

		selected := sessions[sessionNum-1]
		sessionID := selected.SessionID
		state, err := chatAgent.LoadStateScoped(sessionID, selected.WorkingDirectory)
		if err != nil {
			return fmt.Errorf("failed to load session: %w", err)
		}

		chatAgent.ApplyState(state)
		console.GlyphSuccess.Printf("Conversation session loaded: %s", sessionID)
		return nil
	}

	// If no args and no agent (e.g., in tests), just print sessions and return
	if chatAgent == nil {
		fmt.Printf("Found %d saved sessions. Run with a session number to load.\n", len(sessions))
		return nil
	}

	return c.selectSessionWithPicker(sessions, chatAgent)
}

// buildSessionPickerItems renders sessions into picker rows: the name (or
// first-message fallback) as the label, the topic summary and timestamp as
// the detail line. Reversed so the most recent session is at the top.
func buildSessionPickerItems(sessions []agent.SessionInfo) []console.SelectItem {
	items := make([]console.SelectItem, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		name := session.Name
		if name == "" {
			name = agent.GetSessionLabelScoped(session.SessionID, session.WorkingDirectory)
		}
		if name == "" {
			name = session.SessionID
		}
		items = append(items, console.SelectItem{
			Label:  name,
			Detail: strings.TrimSpace(strings.Join([]string{agent.SessionTopicSummaryForDisplay(session), session.LastUpdated.Format("2006-01-02 15:04")}, " · ")),
			Value:  session.SessionID,
		})
	}
	return items
}

// selectSessionWithPicker drives the unified SelectList picker for /sessions.
// Sessions are presented newest-first; selection routes through
// LoadStateScoped + ApplyState exactly like the legacy numeric flow.
// Each row is name (label) + topic summary and timestamp (detail); the
// summary is the disambiguating line, so the label no longer carries the
// 50-rune first-message cap.
func (c *SessionsCommand) selectSessionWithPicker(sessions []agent.SessionInfo, chatAgent *agent.Agent) error {
	items := buildSessionPickerItems(sessions)

	picker := console.NewSelectList(console.SelectListOptions{
		Title:      "Available Sessions",
		Items:      items,
		PageSize:   12,
		Searchable: true,
	})
	chosenID, ok, err := picker.Run(context.Background())
	if err != nil {
		return fmt.Errorf("session picker: %w", err)
	}
	if !ok || chosenID == "" {
		return nil
	}

	var selected agent.SessionInfo
	for _, s := range sessions {
		if s.SessionID == chosenID {
			selected = s
			break
		}
	}
	if selected.SessionID == "" {
		return nil
	}
	state, err := chatAgent.LoadStateScoped(selected.SessionID, selected.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}
	chatAgent.ApplyState(state)
	console.GlyphSuccess.Printf("Conversation session loaded: %s", selected.SessionID)
	return nil
}
