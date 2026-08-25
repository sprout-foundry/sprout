//go:build !js

package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const turnJournalVersion = 1

// turnJournalFsync gates per-append fsync. Off by default: SIGKILL and
// terminal close — the failure modes this journal targets — are covered by
// a plain write() without durability barriers. Power-loss coverage can be
// opted into later via config without changing the format.
var turnJournalFsync = false

type TurnJournalTokens struct {
	TotalTokens      int     `json:"total_tokens,omitempty"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalCost        float64 `json:"total_cost,omitempty"`
}

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

func turnJournalPath(stateFile string) string {
	return stateFile + ".journal.jsonl"
}

func turnJournalPathFor(sessionID, workingDir string) (string, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return "", agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to build session file path")
	}
	return turnJournalPath(stateFile), nil
}

type TurnJournal struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	path string
}

func OpenTurnJournal(sessionID, workingDir string) (*TurnJournal, error) {
	path, err := turnJournalPathFor(sessionID, workingDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, agenterrors.Wrap(err, "failed to create journal directory")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to open turn journal")
	}
	return &TurnJournal{f: f, w: bufio.NewWriter(f), path: path}, nil
}

func (j *TurnJournal) AppendTurnEvent(ev TurnJournalEvent) error {
	if j == nil {
		return nil
	}
	ev.V = turnJournalVersion
	if ev.Ts.IsZero() {
		ev.Ts = time.Now()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return agenterrors.Wrap(err, "failed to marshal journal event")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.w == nil {
		return agenterrors.NewAgent("turn-journal", "journal already closed", nil)
	}
	if _, err := j.w.Write(append(line, '\n')); err != nil {
		return agenterrors.Wrap(err, "failed to write journal event")
	}
	if err := j.w.Flush(); err != nil {
		return agenterrors.Wrap(err, "failed to flush journal event")
	}
	if turnJournalFsync && j.f != nil {
		if err := j.f.Sync(); err != nil {
			return agenterrors.Wrap(err, "failed to sync journal")
		}
	}
	return nil
}

func (j *TurnJournal) CloseTurnJournal() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.w != nil {
		if err := j.w.Flush(); err != nil {
			return agenterrors.Wrap(err, "failed to flush journal on close")
		}
		j.w = nil
	}
	if j.f != nil {
		if err := j.f.Close(); err != nil {
			return agenterrors.Wrap(err, "failed to close journal file")
		}
		j.f = nil
	}
	return nil
}

func RemoveTurnJournal(sessionID, workingDir string) error {
	path, err := turnJournalPathFor(sessionID, workingDir)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return agenterrors.Wrap(err, "failed to remove turn journal")
	}
	return nil
}

func turnJournalExists(sessionID, workingDir string) bool {
	path, err := turnJournalPathFor(sessionID, workingDir)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
