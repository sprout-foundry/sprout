package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// RepairReport summarizes what RepairMessageTail changed.
type RepairReport struct {
	DroppedToolResults         int
	StrippedAssistantToolCalls int
}

// RepairMessageTail fixes provider-breaking tool-exchange shapes at the end
// of a message list: tool results whose tool_call_id no longer matches an
// assistant tool call are dropped, and trailing assistant tool_calls with
// no matching results are stripped (keeping the assistant text if any).
// Only the tail is examined — full-history reconciliation is not the goal.
func RepairMessageTail(msgs []api.Message) ([]api.Message, RepairReport) {
	var report RepairReport
	if len(msgs) == 0 {
		return msgs, report
	}

	called := make(map[string]bool)
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				called[tc.ID] = true
			}
		}
	}

	repaired := make([]api.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" && !called[m.ToolCallID] {
			report.DroppedToolResults++
			continue
		}
		repaired = append(repaired, m)
	}

	for i := len(repaired) - 1; i >= 0; i-- {
		m := repaired[i]
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			if m.Role == "assistant" && strings.TrimSpace(m.Content) == "" && strings.TrimSpace(m.ReasoningContent) == "" && len(m.ToolCalls) == 0 && i == len(repaired)-1 {
				repaired = repaired[:i]
			}
			continue
		}
		var kept []api.ToolCall
		for _, tc := range m.ToolCalls {
			if toolResultPresent(repaired, i, tc.ID) {
				kept = append(kept, tc)
				continue
			}
			report.StrippedAssistantToolCalls++
		}
		if len(kept) == len(m.ToolCalls) {
			break
		}
		if strings.TrimSpace(m.Content) != "" || len(kept) > 0 {
			m.ToolCalls = kept
			repaired[i] = m
			break
		}
		repaired = repaired[:i]
	}

	return repaired, report
}

func toolResultPresent(msgs []api.Message, assistantIdx int, toolCallID string) bool {
	for j := assistantIdx + 1; j < len(msgs); j++ {
		if msgs[j].Role == "tool" && msgs[j].ToolCallID == toolCallID {
			return true
		}
		if msgs[j].Role == "assistant" {
			return false
		}
	}
	return false
}

// RecoveryReport describes what load-time recovery applied.
type RecoveryReport struct {
	JournalReplayed bool
	JournalEvents   int
	InterruptedAt   *time.Time
	Repair          RepairReport
}

// LoadStateRecoverable loads a session and, when a turn journal survives,
// replays it onto the base state. A partial final journal line (crash
// mid-append) is tolerated and ignored.
func LoadStateRecoverable(sessionID, workingDir string) (*ConversationState, RecoveryReport, error) {
	var report RecoveryReport

	state, err := LoadStateWithoutAgentScoped(sessionID, workingDir)
	if err != nil {
		return nil, report, err
	}

	events, ok := readTurnJournalIfExists(sessionID, workingDir)
	if !ok {
		return state, report, nil
	}

	msgs := state.Messages
	var checkpoints []TurnCheckpoint
	applied := 0
	var lastTs time.Time
	for _, ev := range events {
		if !ev.Ts.IsZero() {
			lastTs = ev.Ts
		}
		switch ev.Type {
		case "messages":
			if ev.Base >= 0 && ev.Base <= len(msgs) {
				msgs = msgs[:ev.Base]
			}
			msgs = append(msgs, ev.Msgs...)
			applied++
		case "turn_checkpoint":
			if ev.Checkpoint != nil {
				checkpoints = append(checkpoints, *ev.Checkpoint)
			}
			applied++
		case "token_totals":
			if ev.TokenTotals != nil {
				state.TotalTokens = ev.TokenTotals.TotalTokens
				state.PromptTokens = ev.TokenTotals.PromptTokens
				state.CompletionTokens = ev.TokenTotals.CompletionTokens
				state.TotalCost = ev.TokenTotals.TotalCost
			}
			applied++
		case "turn_start":
			applied++
		}
	}

	state.Messages = msgs
	if len(checkpoints) > 0 {
		state.TurnCheckpoints = mergeRecoveredCheckpoints(state.TurnCheckpoints, checkpoints)
	}
	report.JournalReplayed = applied > 0
	report.JournalEvents = applied

	if !lastTs.IsZero() {
		interrupted := lastTs
		report.InterruptedAt = &interrupted
	} else {
		now := time.Now()
		report.InterruptedAt = &now
	}

	var repair RepairReport
	state.Messages, repair = RepairMessageTail(state.Messages)
	report.Repair = repair

	state.InterruptedAt = report.InterruptedAt
	state.RecoveredFromJournal = report.JournalReplayed
	return state, report, nil
}

func mergeRecoveredCheckpoints(existing, recovered []TurnCheckpoint) []TurnCheckpoint {
	byID := make(map[string]bool, len(existing))
	for _, cp := range existing {
		if cp.ID != "" {
			byID[cp.ID] = true
		}
	}
	merged := append([]TurnCheckpoint(nil), existing...)
	for _, cp := range recovered {
		if cp.ID != "" && byID[cp.ID] {
			continue
		}
		merged = append(merged, cp)
	}
	return merged
}

// readTurnJournalIfExists returns parsed journal events, or ok=false when no
// journal or an unreadable one exists. A truncated final line is ignored —
// a crash mid-append leaves a partial record and that is expected.
func readTurnJournalIfExists(sessionID, workingDir string) ([]TurnJournalEvent, bool) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, false
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return nil, false
	}
	path := turnJournalPath(stateFile)
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil && len(lines) == 0 {
		return nil, false
	}

	events := make([]TurnJournalEvent, 0, len(lines))
	for _, line := range lines {
		var ev TurnJournalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, true
}

const recoveredSessionSupplement = "## Recovered Session\n\nThis session was interrupted mid-turn and restored from its turn journal. " +
	"State is recovered to the last completed tool iteration. The working tree may contain " +
	"edits from the lost portion of the turn — verify with list_changes / view_history before redoing work."

// ApplyRecoveredState applies a recovered state and primes a system
// supplement so the model knows the session was interrupted.
func (a *Agent) ApplyRecoveredState(state *ConversationState) RecoveryReport {
	report := RecoveryReport{
		JournalReplayed: state.RecoveredFromJournal,
		InterruptedAt:   state.InterruptedAt,
	}
	a.ApplyState(state)
	if a.state.GetPendingSystemSupplement() == "" {
		a.setPendingSystemSupplement(recoveredSessionSupplement)
	}
	return report
}
