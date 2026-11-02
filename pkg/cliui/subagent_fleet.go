//go:build !js

package cliui

// subagent_fleet.go — one spinner line for subagents running alongside each
// other (SP-174c).
//
// Parallel and background subagents do not print tool lines: N of them plus
// the primary would interleave row by row, and they share one spinner and
// one collapse state, so their lines also overwrote each other. Their tool
// events update a per-run entry here instead, and the spinner shows the
// whole fleet: "3 subagents · coder:auth read_file · reviewer:ui thinking".
// Each run closes with its own completion block, printed by the runner.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type fleetEntry struct {
	label string // "persona:taskID"
	tool  string // running tool, "" while the run is thinking
}

// subagentFleet tracks the concurrent runs a turn has in flight, in the
// order they first appeared.
type subagentFleet struct {
	mu    sync.Mutex
	order []string
	runs  map[string]*fleetEntry
}

// isConcurrentSubagentEvent reports whether an event comes from a parallel
// or background subagent run.
func isConcurrentSubagentEvent(data map[string]interface{}) bool {
	concurrent, _ := data[agent.EventMetaSubagentConcurrent].(bool)
	return concurrent
}

func (f *subagentFleet) entryLocked(data map[string]interface{}) *fleetEntry {
	id, _ := data[agent.EventMetaSubagentTaskID].(string)
	if f.runs == nil {
		f.runs = map[string]*fleetEntry{}
	}
	if e, ok := f.runs[id]; ok {
		return e
	}
	label := ReadEventPersona(data)
	if id != "" {
		label += ":" + id
	}
	e := &fleetEntry{label: label}
	f.runs[id] = e
	f.order = append(f.order, id)
	return e
}

// toolStarted records a run's tool and returns the fleet's spinner label.
func (f *subagentFleet) toolStarted(data map[string]interface{}) string {
	name, _ := data["tool_name"].(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entryLocked(data).tool = name
	return f.labelLocked()
}

// toolEnded marks a run as thinking again and returns the spinner label.
func (f *subagentFleet) toolEnded(data map[string]interface{}) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entryLocked(data).tool = ""
	return f.labelLocked()
}

// finished drops a run and reports how many are still running.
func (f *subagentFleet) finished(taskID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[taskID]; ok {
		delete(f.runs, taskID)
		for i, id := range f.order {
			if id == taskID {
				f.order = append(f.order[:i], f.order[i+1:]...)
				break
			}
		}
	}
	return len(f.runs)
}

func (f *subagentFleet) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs, f.order = nil, nil
}

func (f *subagentFleet) labelLocked() string {
	parts := []string{fmt.Sprintf("%d %s", len(f.order), pluralWord(len(f.order), "subagent", "subagents"))}
	for _, id := range f.order {
		e := f.runs[id]
		activity := e.tool
		if activity == "" {
			activity = "thinking"
		}
		parts = append(parts, e.label+" "+activity)
	}
	return strings.Join(parts, " · ")
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// handleFleetToolEvent folds a concurrent run's tool event into the fleet
// status. Nothing reaches the transcript and the primary's collapse state
// is untouched, since no row was printed.
//
// Between turns (a background run working while the user types) the status
// is kept but not drawn: the spinner would land on the prompt row.
func (s *TerminalSubscriberState) handleFleetToolEvent(start bool, data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	var label string
	if start {
		label = s.fleet.toolStarted(data)
	} else {
		label = s.fleet.toolEnded(data)
	}
	if s.IsCompact() || footer.IdleBetweenTurns() {
		return
	}
	indicator.Start(label)
}

// fleetRunFinished retires a run. With siblings still running the spinner
// shows the smaller fleet; with none, it goes back to the primary's own
// in-flight tool, or stops, so a finished fleet is not left on screen.
func (s *TerminalSubscriberState) fleetRunFinished(taskID string, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	if taskID == "" || !s.fleet.has(taskID) {
		return
	}
	remaining := s.fleet.finished(taskID)
	if s.IsCompact() || footer.IdleBetweenTurns() {
		return
	}
	if remaining > 0 {
		indicator.Start(s.fleet.label())
		return
	}
	for _, label := range s.toolsInFlight {
		indicator.Start(label)
		return
	}
	indicator.Stop()
}

func (f *subagentFleet) has(taskID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.runs[taskID]
	return ok
}

// label is labelLocked for callers not holding the lock.
func (f *subagentFleet) label() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.labelLocked()
}

// handleConcurrentSubagentEvent routes an event from a parallel or
// background run. Tool events feed the fleet status; activity, prompts and
// agent messages (warnings, security cautions) are handled as usual; the
// rest — stream chunks, query_started, todo panels — would only interleave
// with the primary's transcript, and the run's completion block covers them.
func (s *TerminalSubscriberState) handleConcurrentSubagentEvent(evtType string, data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	switch evtType {
	case events.EventTypeToolStart:
		s.handleFleetToolEvent(true, data, indicator, footer)
	case events.EventTypeToolEnd:
		s.handleFleetToolEvent(false, data, indicator, footer)
		footer.Refresh()
	case events.EventTypeSubagentActivity:
		s.HandleSubagentActivityEvent(data, indicator, footer)
	case events.EventTypeSecurityApprovalRequest,
		events.EventTypeSecurityPromptRequest,
		events.EventTypeAskUserRequest:
		s.HandleSecurityPromptEvent(indicator)
	case events.EventTypeAgentMessage:
		s.HandleAgentMessageEvent(data, indicator, footer)
	}
}
