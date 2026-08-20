package agent

import (
	"fmt"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// beginTurnJournal opens the session's turn journal and records turn_start.
// The journal captures mid-turn state so a hard-killed process leaves enough
// on disk to reconstruct the turn up to its last completed iteration.
// Failures are logged and non-fatal — journalling must never break a turn.
func (a *Agent) beginTurnJournal(query string) {
	if a == nil || a.state == nil || a.IsSubagent() {
		return
	}
	a.journalMu.Lock()
	defer a.journalMu.Unlock()
	if a.turnJournal != nil {
		return
	}
	sessionID := a.state.GetSessionID()
	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d", time.Now().Unix())
		a.state.SetSessionID(sessionID)
	}
	j, err := OpenTurnJournal(sessionID, a.currentWorkspaceRoot())
	if err != nil {
		a.Logger().Debug("[WARN] failed to open turn journal: %v\n", err)
		return
	}
	if j == nil {
		return
	}
	a.turnJournal = j
	a.journalBase = len(a.state.GetMessages())
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type:  "turn_start",
		Query: query,
		Base:  a.journalBase,
	}); err != nil {
		a.Logger().Debug("[WARN] failed to append turn_start: %v\n", err)
	}
}

// journalSeedState appends a messages event (delta since the journal
// high-water mark) and a token_totals event from seed's live state. Called
// from OnIteration at every iteration start and from the end-of-turn sync
// points in handleQueryResult. seed's list is the full history (base +
// turn), so only messages beyond the mark are recorded; the mark advances
// so each call appends only its own delta.
func (a *Agent) journalSeedState(st *core.State) {
	if a == nil || st == nil {
		return
	}
	seedMsgs := st.Messages()
	a.journalMu.Lock()
	j := a.turnJournal
	base := a.journalBase
	a.journalMu.Unlock()
	if j == nil || len(seedMsgs) <= base {
		return
	}
	newMsgs := append([]api.Message(nil), seedMsgs[base:]...)
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type: "messages",
		Base: base,
		Msgs: newMsgs,
	}); err != nil {
		a.Logger().Debug("[WARN] failed to append journal messages: %v\n", err)
		return
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type: "token_totals",
		TokenTotals: &TurnJournalTokens{
			TotalTokens: st.TotalTokens(),
			TotalCost:   st.TotalCost(),
		},
	}); err != nil {
		a.Logger().Debug("[WARN] failed to append journal token totals: %v\n", err)
	}
	a.journalMu.Lock()
	if len(seedMsgs) > a.journalBase {
		a.journalBase = len(seedMsgs)
	}
	a.journalMu.Unlock()
}

// journalTurnCheckpoint appends a checkpoint event. Called from the
// checkpoint path, including its background goroutine.
func (a *Agent) journalTurnCheckpoint(cp TurnCheckpoint) {
	a.journalMu.Lock()
	j := a.turnJournal
	a.journalMu.Unlock()
	if j == nil {
		return
	}
	cpCopy := cp
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type:       "turn_checkpoint",
		Checkpoint: &cpCopy,
	}); err != nil {
		a.Logger().Debug("[WARN] failed to append journal checkpoint: %v\n", err)
	}
}

// endTurnJournal closes the journal handle. The journal FILE is removed by
// finalizeTurnJournal only after the turn-boundary state save succeeds —
// a surviving journal is the interrupted-session signal.
func (a *Agent) endTurnJournal() {
	a.journalMu.Lock()
	j := a.turnJournal
	a.turnJournal = nil
	a.journalMu.Unlock()
	if j != nil {
		if err := j.CloseTurnJournal(); err != nil {
			a.Logger().Debug("[WARN] failed to close turn journal: %v\n", err)
		}
	}
}

// finalizeTurnJournal removes the journal after a successful turn-boundary
// save. Called from the autoSaveState path.
func (a *Agent) finalizeTurnJournal() {
	if a == nil || a.state == nil || a.IsSubagent() {
		return
	}
	sessionID := a.state.GetSessionID()
	if sessionID == "" {
		return
	}
	if err := RemoveTurnJournal(sessionID, a.currentWorkspaceRoot()); err != nil {
		a.Logger().Debug("[WARN] failed to remove turn journal: %v\n", err)
	}
}
