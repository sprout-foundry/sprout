//go:build js

package agent

import (
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// WASM stubs — append-only file journals are not meaningful in the browser
// sandbox. The lifecycle helpers compile so wiring code stays tag-free;
// OpenTurnJournal returns nil and every method tolerates a nil receiver.

type TurnJournal struct{}

type TurnJournalEvent struct {
	V           int                `json:"v"`
	Type        string             `json:"type"`
	Ts          time.Time          `json:"ts"`
	Query       string             `json:"query,omitempty"`
	Base        int                `json:"base,omitempty"`
	Msgs        []api.Message      `json:"msgs,omitempty"`
	Checkpoint  *TurnCheckpoint    `json:"checkpoint,omitempty"`
	TokenTotals *TurnJournalTokens `json:"token_totals,omitempty"`
}

type TurnJournalTokens struct {
	TotalTokens      int     `json:"total_tokens,omitempty"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalCost        float64 `json:"total_cost,omitempty"`
}

func OpenTurnJournal(sessionID, workingDir string) (*TurnJournal, error) {
	return nil, nil
}

func (j *TurnJournal) AppendTurnEvent(ev TurnJournalEvent) error { return nil }

func (j *TurnJournal) CloseTurnJournal() error { return nil }

func RemoveTurnJournal(sessionID, workingDir string) error { return nil }

func turnJournalPath(stateFile string) string { return stateFile + ".journal.jsonl" }

func turnJournalExists(sessionID, workingDir string) bool { return false }
