// tool_repeat_guard.go — the per-turn repeat guard generalized to every tool
// plus the stall stop. A coordinator can hammer the same tool call (e.g. a
// shell_command probe) for minutes without making progress; the read_file
// guard (tool.1) covered only that one tool. This layer:
//
//   - counts identical tool calls (tool name + normalised arguments) within
//     a single turn, and from the 3rd identical call prefixes the model-
//     visible result with a note that the call has now run N times and that
//     repeating it will not change the outcome;
//   - detects a stall (the identical-call or idle-call threshold) so the
//     turn can end early with a "stalled" stop reason, a run-record summary,
//     and an event the runner UI shows;
//   - exposes the stall summary so the workflow continuation loop can treat
//     the stalled turn as a no-progress turn and steer the next turn.
//
// The read_file-specific ReadCallGuard/ObserveRead path is left untouched;
// the general guard also observes read_file calls (its note composes with
// read_file's own short-circuit note), which is acceptable and keeps one
// uniform rule for every tool.

package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ErrTurnStalled is the typed error a turn returns when the per-turn repeat
// guard cuts it short (a call crossed a stall threshold). It is NOT a fatal
// failure: the workflow continuation loop recognises it (errors.Is) and treats
// the turn as a no-progress turn — steering the next turn with a stall prompt
// — rather than stopping the run. It is distinct from a user interrupt and
// from a real turn error.
var ErrTurnStalled = errors.New("turn stalled: repeated tool calls made no progress")

// The per-turn repeat-guard stall thresholds for standalone/interactive
// sessions (no workflow config override). They mirror the workflow config
// defaults (pkg/workflow's DefaultStallIdenticalCalls / DefaultStallIdleCalls,
// applied via AgentWorkflowConfig's stall_identical_calls / stall_idle_calls)
// so the two stay in lockstep: 8 identical calls, 60 idle calls.
const (
	defaultStallIdenticalCalls = 8
	defaultStallIdleCalls      = 60
)

// stallSummaryOutputBytes caps the "last output" excerpt carried in a stall
// summary (and the agent_turn_stalled event) so a huge repeated result does
// not bloat the run record or the event payload.
const stallSummaryOutputBytes = 2048

// repeatNoteMinCount is the call count at which the repeat note starts
// appearing in the model-visible result (the 3rd identical call).
const repeatNoteMinCount = 3

// wsCollapseRe matches runs of horizontal whitespace used to normalise
// tool-call arguments (so `make  build` and `make build` share a key).
var wsCollapseRe = regexp.MustCompile(`[ \t\r\n]+`)

// toolChannelSuffixRe strips seed's per-channel tool-name suffix
// (`<|channel|>N`) so a tool reached through different channels still shares
// one repeat key. Mirrors seed's own stripChannelSuffix.
var toolChannelSuffixRe = regexp.MustCompile(`<\|channel\|\>\d+$`)

// stripToolChannel removes a trailing seed channel suffix from a tool name so
// the repeat key is stable across channels.
func stripToolChannel(name string) string {
	return toolChannelSuffixRe.ReplaceAllString(name, "")
}

// normalizeToolArgs collapses whitespace runs to a single space and trims the
// ends so cosmetically-different but semantically-identical arguments hash
// to the same key.
func normalizeToolArgs(args string) string {
	return strings.TrimSpace(wsCollapseRe.ReplaceAllString(args, " "))
}

// toolArgKey is the per-turn repeat-tracker key for one (tool, normalised
// args) pair. The tool name is channel-stripped so a tool reached through
// different seed channels shares one key.
func toolArgKey(toolName, args string) string {
	return stripToolChannel(toolName) + "\x00" + normalizeToolArgs(args)
}

// digestOf is a cheap content digest used for the byte-identical check (the
// full result is compared by digest, not stored verbatim, so large repeated
// results do not pile up in memory).
func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// stallSummaryTrunc truncates s to max bytes, appending a marker when it cuts
// so a reader knows the excerpt is partial.
func stallSummaryTrunc(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf(" …(truncated, %d bytes total)", len(s))
}

// toolRepeatState is the per-(tool,args)-key state: how many times the exact
// call has run this turn, the digest of its last result (for the
// byte-identical note), and a truncated copy of the last result (for the
// stall summary).
type toolRepeatState struct {
	count      int
	lastDigest string
	lastOutput string
}

// toolRepeatTracker counts identical tool calls within one turn and exposes
// the aggregate the stall stop needs. It is owned by the Agent (guarded by
// the agent's stallMu, like fileReadsMu guards the read tracker) and reset
// at each turn boundary.
type toolRepeatTracker struct {
	mu            sync.Mutex
	keys          map[string]*toolRepeatState
	totalCalls    int
	distinctCalls int
}

func newToolRepeatTracker() *toolRepeatTracker {
	return &toolRepeatTracker{keys: make(map[string]*toolRepeatState)}
}

// observe records one tool call under key and returns the call's 1-based
// count for this key and whether its result is byte-identical to the
// previous identical call's result (false on the first call). It is the
// single bookkeeping site, so the count, the identical flag, and the
// aggregate can never diverge.
func (t *toolRepeatTracker) observe(key, result string) (count int, identical bool) {
	if t == nil {
		return 1, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.keys == nil {
		t.keys = make(map[string]*toolRepeatState)
	}
	st, ok := t.keys[key]
	if !ok {
		st = &toolRepeatState{}
		t.keys[key] = st
		t.distinctCalls++
	}
	st.count++
	count = st.count
	t.totalCalls++
	identical = st.lastDigest != "" && digestOf(result) == st.lastDigest
	st.lastDigest = digestOf(result)
	st.lastOutput = stallSummaryTrunc(result, stallSummaryOutputBytes)
	return count, identical
}

// idleCalls returns how many calls this turn repeated an already-seen
// (tool,args) key — the number of non-new (redundant) calls. A turn that
// keeps introducing distinct commands keeps this near zero; a run that
// hammers one probe drives it up. A session that is making real progress
// (edits, new commands) grows distinctCalls, so this stays low.
func (t *toolRepeatTracker) idleCalls() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalCalls - t.distinctCalls
}

// keyCount returns how many times a specific key has run this turn (0 when
// unseen).
func (t *toolRepeatTracker) keyCount(key string) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.keys[key]; ok {
		return st.count
	}
	return 0
}

// lastOutputFor returns the truncated last output stored for a key ("" when
// unseen). Used to build the stall summary.
func (t *toolRepeatTracker) lastOutputFor(key string) string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.keys[key]; ok {
		return st.lastOutput
	}
	return ""
}

// reset clears all per-turn state. Called at each turn boundary so a repeat
// in a NEW turn is a first call.
func (t *toolRepeatTracker) reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keys = make(map[string]*toolRepeatState)
	t.totalCalls = 0
	t.distinctCalls = 0
}

// StallSummary describes a turn that ended on the repeat guard's stall stop:
// which call was repeated, its normalised arguments, an excerpt of its last
// output, and how many times it ran. The workflow continuation loop maps it
// to a no-progress turn; a nil *StallSummary means the turn did not stall.
type StallSummary struct {
	// Tool is the repeated tool name (e.g. "shell_command").
	Tool string `json:"tool"`
	// Args is the normalised argument string for the repeated call.
	Args string `json:"args,omitempty"`
	// Output is a truncated excerpt of the repeated call's last output.
	Output string `json:"output,omitempty"`
	// Repeats is how many times the call has run this turn.
	Repeats int `json:"repeats"`
	// Reason classifies the stall: "identical_calls" (one call hit the
	// identical-call threshold) or "idle_calls" (a stretch of redundant
	// calls hit the idle-call threshold).
	Reason string `json:"reason,omitempty"`
}

// toolRepeatTrackerForTurn returns the agent's per-turn repeat tracker,
// creating it lazily. Nil-safe.
func (a *Agent) toolRepeatTrackerForTurn() *toolRepeatTracker {
	if a == nil {
		return nil
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	if a.toolRepeatTracker == nil {
		a.toolRepeatTracker = newToolRepeatTracker()
	}
	return a.toolRepeatTracker
}

// stallIdenticalThreshold returns the configured identical-call stall
// threshold (the package default when unset).
func (a *Agent) stallIdenticalThreshold() int {
	if a == nil {
		return defaultStallIdenticalCalls
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	if a.stallIdenticalCalls <= 0 {
		return defaultStallIdenticalCalls
	}
	return a.stallIdenticalCalls
}

// stallIdleThreshold returns the configured idle-call stall threshold (the
// package default when unset).
func (a *Agent) stallIdleThreshold() int {
	if a == nil {
		return defaultStallIdleCalls
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	if a.stallIdleCalls <= 0 {
		return defaultStallIdleCalls
	}
	return a.stallIdleCalls
}

// recordToolCall observes one tool call in the per-turn repeat tracker and
// returns the repeat-note prefix (empty when the call has not yet hit the
// note threshold), whether this call crossed a stall threshold, and — when it
// did — the stall summary to record on the turn. It is called by the
// executor wrapper around the seed tool registry, so every tool call is
// observed.
func (a *Agent) recordToolCall(toolName, args, result string) (note string, stalled bool, summary *StallSummary) {
	if a == nil {
		return "", false, nil
	}
	tracker := a.toolRepeatTrackerForTurn()
	if tracker == nil {
		return "", false, nil
	}
	// Strip the channel suffix so the note/summary name the bare tool.
	tool := stripToolChannel(toolName)
	key := toolArgKey(toolName, args)
	count, identical := tracker.observe(key, result)
	normArgs := normalizeToolArgs(args)

	if count >= repeatNoteMinCount {
		note = toolRepeatNote(tool, normArgs, count, identical)
	}

	switch {
	case count >= a.stallIdenticalThreshold():
		stalled = true
		summary = &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(key),
			Repeats: count,
			Reason:  "identical_calls",
		}
	case tracker.idleCalls() >= a.stallIdleThreshold() && len(a.TurnChangedPaths()) == 0:
		// A long stretch of redundant calls with no new distinct command
		// AND no file change this turn is a stall (the model is spinning,
		// not making progress). A turn that edits files is making progress,
		// so it never trips here. A read-only turn whose calls keep
		// introducing new distinct commands never accumulates redundancy,
		// so a genuinely varied investigation is safe; cycling among a few
		// commands (the loop shape this guards) trips, and costs one steered
		// continuation turn — the thresholds are configurable per workflow.
		stalled = true
		summary = &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(key),
			Repeats: tracker.idleCalls(),
			Reason:  "idle_calls",
		}
	}
	return note, stalled, summary
}

// toolRepeatNote builds the repeat-nudge prefix prepended to a model-visible
// result once an exact call has run repeatNoteMinCount+ times this turn. It
// names the call and its count, flags byte-identical output when that is the
// case, and tells the model to change tack or stop rather than re-issue.
func toolRepeatNote(tool, args string, count int, identicalResult bool) string {
	var b strings.Builder
	if strings.TrimSpace(args) != "" {
		snippet := args
		if len(snippet) > 80 {
			snippet = snippet[:77] + "..."
		}
		fmt.Fprintf(&b, "[repeat guard] %s with %s has now run %d times this turn with these arguments.", tool, snippet, count)
	} else {
		fmt.Fprintf(&b, "[repeat guard] %s has now run %d times this turn with these arguments.", tool, count)
	}
	if identicalResult {
		b.WriteString(" The output is byte-identical to the previous identical call.")
	}
	b.WriteString(" Repeating it will not change the outcome — change the command, inspect a different signal, or record the blocker and stop.")
	return b.String()
}

// SetStallThresholds configures the per-turn repeat-guard stall thresholds.
// Non-positive values fall back to the package defaults (8 identical calls,
// 60 idle calls). Nil-safe; the workflow runner calls it from the workflow
// config's stall_identical_calls / stall_idle_calls so a coordinator can
// tune the thresholds per run.
func (a *Agent) SetStallThresholds(identicalCalls, idleCalls int) {
	if a == nil {
		return
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	a.stallIdenticalCalls = identicalCalls
	a.stallIdleCalls = idleCalls
}

// ResetToolRepeatGuardForNewTurn clears the per-turn tool-repeat tracker (and
// the stall-stop callback) at turn boundaries so a repeat in a new turn is a
// first call. It is called from the same turn-open point as
// ResetFileReadsForNewTurn. It does NOT clear the stall summary: the
// workflow continuation loop still needs to read the just-finished turn's
// summary; that is cleared explicitly (ClearStallSummary).
func (a *Agent) ResetToolRepeatGuardForNewTurn() {
	if a == nil {
		return
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	if a.toolRepeatTracker == nil {
		a.toolRepeatTracker = newToolRepeatTracker()
	}
	a.toolRepeatTracker.reset()
	a.stallStopFn = nil
}

// ClearStallSummary clears the stored stall summary. Called at the start of
// each turn (prepareQueryRun) and by the continuation loop after it consumes
// a stalled turn's summary, so a stale summary never leaks into a later turn.
// Nil-safe.
func (a *Agent) ClearStallSummary() {
	if a == nil {
		return
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	a.lastStallSummary = nil
}

// GetStallSummary returns the current turn's stall summary (nil when the
// turn did not stall). Nil-safe for bare Agents. The workflow continuation
// loop reads it after a turn to detect a stall and steer the next turn.
func (a *Agent) GetStallSummary() *StallSummary {
	if a == nil {
		return nil
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	return a.lastStallSummary
}

// markTurnStalled records a stall on the current turn: it stores the summary
// for the continuation loop / run record, sets the "stalled" termination
// reason, and publishes the agent_turn_stalled event for the runner UI. It is
// idempotent — calling it more than once for the same turn re-asserts the
// same state.
func (a *Agent) markTurnStalled(s *StallSummary) {
	if a == nil || s == nil {
		return
	}
	a.stallMu.Lock()
	a.lastStallSummary = s
	a.stallMu.Unlock()

	if a.state != nil {
		a.state.SetLastRunTerminationReason(RunTerminationStalled)
	}
	a.publishEvent(events.EventTypeAgentTurnStalled, events.AgentTurnStalledEvent(
		s.Tool, s.Args, s.Output, s.Repeats, s.Reason))
}

// RecordStallSummary stores s as the turn's stall summary so the workflow
// continuation layer (and its tests) can read it via GetStallSummary and
// treat the turn as a no-progress turn. It mirrors the in-turn guard's
// markTurnStalled bookkeeping but WITHOUT the termination-reason write or
// the event publish (those belong to the in-turn path) — it only records
// the summary for later reading. Nil-safe.
func (a *Agent) RecordStallSummary(s *StallSummary) {
	if a == nil || s == nil {
		return
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	a.lastStallSummary = s
}

// setStallStop installs the callback that ends the in-flight turn's
// conversation loop (by cancelling the run context). Called by prepareQuery
// Run for the duration of a turn and cleared at the turn boundary.
func (a *Agent) setStallStop(fn func()) {
	if a == nil {
		return
	}
	a.stallMu.Lock()
	defer a.stallMu.Unlock()
	a.stallStopFn = fn
}

// requestStallStop ends the in-flight turn by invoking the installed stall-
// stop callback (cancelling the run context). It is called by the executor
// wrapper when a call crosses a stall threshold. Nil-safe and a no-op when no
// turn is in flight (no callback installed).
func (a *Agent) requestStallStop() {
	if a == nil {
		return
	}
	a.stallMu.Lock()
	fn := a.stallStopFn
	a.stallMu.Unlock()
	if fn != nil {
		fn()
	}
}

// turnStalledThisRun reports whether the current run's turn was cut by the
// repeat guard's stall stop (a stall summary was recorded for this turn).
// handleQueryResult uses it to keep the "stalled" termination reason (rather
// than reclassifying the run-context cancellation as a user interrupt) and
// to soft-return the partial result so the continuation loop can treat the
// turn as a no-progress turn.
func (a *Agent) turnStalledThisRun() bool {
	return a.GetStallSummary() != nil
}
