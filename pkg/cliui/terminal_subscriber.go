//go:build !js

package cliui

// terminal_subscriber.go — the terminal subscriber state + tool
// start / end display: the security / verbose identity labels, the
// TerminalSubscriberState type and its IsCompact / IsVerbose / VerboseMaxArgLen
// accessors, MaybeDisplayEditDiff, NewTerminalSubscriberState,
// flushExternalWrite, ResetSpawnTurn, and the HandleToolStartEvent /
// HandleToolEndEvent handlers. The query / stream / subagent / security /
// todo / message event handlers and the event loop live in
// terminal_subscriber_events.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SecurityCautionLabel is the bracketed label rendered in security caution
// messages. CLI-B-2 extraction.
const SecurityCautionLabel = "SECURITY CAUTION"

// SecurityLoopLabel is the bracketed label rendered in security loop
// messages. CLI-B-2 extraction.
const SecurityLoopLabel = "SECURITY LOOP"

// VerbosePreviewWidth is the argument-preview truncation width in verbose
// mode. In verbose mode the width is bumped so power users see more of
// the path or command.
const VerbosePreviewWidth = 200

// TerminalSubscriberState holds all mutable state for the terminal tool
// subscriber goroutine. Extracted from the closure variables of
// startTerminalToolSubscriber so the event loop can be broken into
// focused handler methods.
type TerminalSubscriberState struct {
	spawnMu          sync.Mutex
	seenSpawn        map[string]bool
	spawnTasks       map[string]string // persona → short task description (CLI-UX-11)
	run              *ToolRunState
	pendingArgs      map[string]string
	progressMu       sync.Mutex
	subagentProgress map[string]SubagentProgressSnapshot
	configMgr        *configuration.Manager // read live for output_verbosity
	// chatAgent is the parent agent, used to flush the
	// AssistantTurnRenderer's buffered prose via
	// OutputRouter.FlushExternalWrite before terminal writes.
	// May be nil in non-agent callers / tests.
	chatAgent *agent.Agent
	// thinkingActive tracks whether the "thinking…" spinner was started
	// by a query_started event and is still showing. It lets
	// HandleStreamChunkEvent know to stop the spinner when assistant
	// prose begins streaming, and HandleToolStartEvent know to stop it
	// when a tool fires before any prose (query_started → ToolStart with
	// no StreamChunk in between).
	thinkingActive bool
	// toolsInFlight maps a running tool's call id to its spinner label.
	// Tools the model calls in parallel share the one spinner row; see
	// startToolSpinner.
	toolsInFlight map[string]string
	// spinnerUnderEndLine is set when the spinner was resumed directly
	// below an end line instead of after a blank row.
	spinnerUnderEndLine bool
}

// IsCompact reports whether the subscriber should suppress tool chrome
// (spinner, result lines, todo blocks, subagent announcements). Read
// live from the config manager on each call so a mid-session
// /settings change takes effect immediately instead of requiring a
// restart. Falls back to false (non-compact) when the manager is nil
// (non-agent callers, tests).
func (s *TerminalSubscriberState) IsCompact() bool {
	if s.configMgr == nil {
		return false
	}
	cfg := s.configMgr.GetConfig()
	if cfg == nil {
		return false
	}
	return cfg.OutputVerbosity == configuration.OutputVerbosityCompact
}

// IsVerbose reports whether the subscriber should show extended detail
// (full tool arguments, result size suffixes). Read live from the config
// manager — mirrors IsCompact() — so a mid-session /settings change to
// "verbose" takes effect immediately without a restart. Falls back to
// false when the manager is nil (non-agent callers, tests).
func (s *TerminalSubscriberState) IsVerbose() bool {
	if s.configMgr == nil {
		return false
	}
	cfg := s.configMgr.GetConfig()
	if cfg == nil {
		return false
	}
	return cfg.OutputVerbosity == configuration.OutputVerbosityVerbose
}

// VerboseMaxArgLen returns the argument-preview truncation width to pass
// to FormatToolPreview/FormatToolArgPreview. In verbose mode the width is
// bumped so power users see more of the path or command. In default or
// compact mode it returns 0, which tells the preview functions to use
// their built-in per-tool defaults.
func (s *TerminalSubscriberState) VerboseMaxArgLen() int {
	if s.IsVerbose() {
		return VerbosePreviewWidth
	}
	return 0
}

// MaybeDisplayEditDiff shows a compact diff for file-editing tools
// (edit_file, write_file). In verbose mode the full diff is shown; in
// default mode it's truncated to EditDiffMaxLines. Compact mode reaches
// this only for errors — successes return early with just the diffstat.
func (s *TerminalSubscriberState) MaybeDisplayEditDiff(toolName, argsJSON string) {
	switch toolName {
	case "edit_file":
		oldStr := extractStrArg(argsJSON, "old_str")
		newStr := extractStrArg(argsJSON, "new_str")
		if oldStr == "" && newStr == "" {
			return
		}
		maxLines := EditDiffMaxLines
		if s.IsVerbose() {
			maxLines = 0 // unlimited
		}
		diff := ComputeEditDiff(oldStr, newStr, maxLines)
		if diff != "" {
			fmt.Fprint(os.Stderr, diff)
		}
	case "write_file":
		content := extractStrArg(argsJSON, "content")
		if content == "" {
			return
		}
		maxLines := EditDiffMaxLines
		if s.IsVerbose() {
			maxLines = 0
		}
		diff := ComputeWriteFileDiff(content, maxLines)
		if diff != "" {
			fmt.Fprint(os.Stderr, diff)
		}
	}
}

// extractStrArg extracts a string field from a JSON argument blob.
// Returns "" if the field is missing or parsing fails.
func extractStrArg(argsJSON, key string) string {
	if argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

// NewTerminalSubscriberState initializes a fresh subscriber state with
// pre-allocated maps and the config manager for live verbosity reads.
func NewTerminalSubscriberState(configMgr *configuration.Manager, chatAgent *agent.Agent) *TerminalSubscriberState {
	return &TerminalSubscriberState{
		seenSpawn:        make(map[string]bool),
		pendingArgs:      make(map[string]string),
		toolsInFlight:    make(map[string]string),
		subagentProgress: make(map[string]SubagentProgressSnapshot),
		configMgr:        configMgr,
		chatAgent:        chatAgent,
	}
}

// flushExternalWrite flushes the AssistantTurnRenderer's buffered prose
// (if any) via the OutputRouter's external-write hook so partial text
// appears inline before tool chrome (spinner, result lines) is written.
// No-op when no agent or router is available.
func (s *TerminalSubscriberState) flushExternalWrite() {
	if s.chatAgent == nil {
		return
	}
	if router := s.chatAgent.OutputRouter(); router != nil {
		router.FlushExternalWrite()
	}
}

// ResetSpawnTurn clears the per-turn spawn dedupe map so the next batch
// of subagents gets fresh announcements. Called by the REPL loop at the
// start of each user turn.
func (s *TerminalSubscriberState) ResetSpawnTurn() {
	s.spawnMu.Lock()
	s.seenSpawn = make(map[string]bool)
	s.spawnTasks = make(map[string]string)
	s.spawnMu.Unlock()
}

// HandleToolStartEvent processes a ToolStart event.
//
// Interactive tools bypass the spinner entirely. For all other tools:
// resolve any active reasoning fold, cache args for the matching ToolEnd,
// announce subagent spawns once per (depth, persona) per turn, and start
// the activity indicator with a context suffix when progress is available.
//
// In "compact" verbosity mode, the spinner and spawn announcements are
// suppressed — only error results break the silence.
func (s *TerminalSubscriberState) HandleToolStartEvent(data map[string]interface{}, chatAgent *agent.Agent, indicator *console.ActivityIndicator) {
	name, _ := data["tool_name"].(string)
	if name == "" {
		return
	}
	if agent.IsInteractiveTool(name) {
		// Tool renders its own prompt — make sure any active
		// spinner is gone before the prompt lands.
		indicator.Stop()
		s.thinkingActive = false
		return
	}
	// CLI-UX-5: A tool is starting — clear the thinking spinner if it
	// was active. The tool spinner (started below) replaces it.
	s.thinkingActive = false
	args, _ := data["arguments"].(string)
	if id, _ := data["tool_call_id"].(string); id != "" && args != "" {
		s.pendingArgs[id] = args
	}
	depth := ReadEventDepth(data)
	persona := ReadEventPersona(data)

	// CLI-UX-11: When run_subagent starts at depth 0, capture the task
	// description from the prompt field so the spawn announcement can
	// show "→ coder: refactoring auth.go" instead of just "→ coder".
	if name == "run_subagent" && depth == 0 && args != "" {
		if taskDesc, subPersona := ExtractSubagentTask(args); taskDesc != "" && subPersona != "" {
			s.spawnMu.Lock()
			if s.spawnTasks == nil {
				s.spawnTasks = make(map[string]string)
			}
			s.spawnTasks[subPersona] = taskDesc
			s.spawnMu.Unlock()
		}
	}

	// Compact mode: suppress spinner start, blank line, and spawn
	// announcements. Return early — the user only wants to see
	// results (tool end lines) when something goes wrong.
	if s.IsCompact() {
		return
	}

	// announce subagent spawn once per (depth,
	// persona) pair per turn, with provider/model so the user
	// can see which cheaper/faster model is doing the work.
	if depth > 0 && persona != "" {
		key := fmt.Sprintf("%d:%s", depth, persona)
		s.spawnMu.Lock()
		announce := !s.seenSpawn[key]
		if announce {
			s.seenSpawn[key] = true
		}
		taskDesc := s.spawnTasks[persona]
		s.spawnMu.Unlock()
		if announce {
			s.flushExternalWrite()
			indicator.Stop()
			s.progressMu.Lock()
			spawnSnap, hasSpawnSnap := s.subagentProgress[persona]
			s.progressMu.Unlock()
			ctxMax := 0
			if hasSpawnSnap {
				ctxMax = spawnSnap.CtxMax
			}
			fmt.Fprintln(os.Stderr, FormatSpawnLine(chatAgent, depth, persona, ctxMax, taskDesc))
		}
	}
	s.progressMu.Lock()
	snap, hasSnap := s.subagentProgress[persona]
	s.progressMu.Unlock()
	ctxSuffix := ""
	if hasSnap && depth > 0 {
		ctxSuffix = FormatSubagentCtxSuffix(snap)
	}
	label := FormatToolStartLine(depth, persona, name, FormatToolPreview(chatAgent, name, args, s.VerboseMaxArgLen())) + ctxSuffix

	// A parallel call joins the spinner that is already running rather
	// than opening a row of its own: the end line of whichever tool
	// finishes first replaces the spinner's row, so any earlier row would
	// be stranded with its first frame.
	parallel := len(s.toolsInFlight) > 0 && indicator.IsActive()
	s.toolsInFlight[toolInFlightKey(data, name)] = label
	if parallel {
		indicator.Start(label)
		return
	}
	s.startToolSpinner(indicator, label)
}

// startToolSpinner starts the tool spinner on a fresh line so it never
// overwrites partial streamed text (stdout, for parity with how stream
// chunks were just printed), leaving a blank row above the spinner.
func (s *TerminalSubscriberState) startToolSpinner(indicator *console.ActivityIndicator, label string) {
	s.flushExternalWrite()
	console.LockOutput()
	_, _ = fmt.Fprintln(os.Stdout)
	console.UnlockOutput()
	s.spinnerUnderEndLine = false
	indicator.Start(label)
}

// resumeToolSpinner puts the spinner back for tools still running after
// one of a parallel batch finished. The end line left the cursor on a
// fresh row, so the spinner goes there directly — no blank row between.
func (s *TerminalSubscriberState) resumeToolSpinner(indicator *console.ActivityIndicator) {
	for _, label := range s.toolsInFlight {
		s.spinnerUnderEndLine = true
		indicator.Start(label)
		return
	}
}

// rowsAboveSpinnerToPreviousEnd is how far ReplaceLastN must walk up from
// the spinner to reach the previous end line: past the blank row a fresh
// spinner leaves, or straight to it for a resumed one.
func (s *TerminalSubscriberState) rowsAboveSpinnerToPreviousEnd() int {
	if s.spinnerUnderEndLine {
		return 1
	}
	return 2
}

func toolInFlightKey(data map[string]interface{}, name string) string {
	if id, _ := data["tool_call_id"].(string); id != "" {
		return id
	}
	return name
}

// HandleToolEndEvent processes a ToolEnd event.
//
// Interactive tools are skipped. For other tools: recover args from the
// ToolStart cache, collapse consecutive identical calls into a single
// in-place row (Phase 3), or emit a fresh end line. Refreshes the footer.
func (s *TerminalSubscriberState) HandleToolEndEvent(data map[string]interface{}, chatAgent *agent.Agent, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	name, _ := data["tool_name"].(string)
	if name == "" {
		return
	}
	if agent.IsInteractiveTool(name) {
		// No spinner was started; emit no result chrome.
		return
	}
	delete(s.toolsInFlight, toolInFlightKey(data, name))
	status, _ := data["status"].(string)
	var durationMs int64
	switch v := data["duration_ms"].(type) {
	case int64:
		durationMs = v
	case float64:
		durationMs = int64(v)
	}
	icon := ToolEndGlyph(name, status).Prefix()
	// ToolEnd doesn't carry arguments; recover them from
	// the ToolStart cache so the collapse-line preview
	// shows real paths instead of empty parens.
	args, _ := data["arguments"].(string)
	if args == "" {
		if id, _ := data["tool_call_id"].(string); id != "" {
			if cached, ok := s.pendingArgs[id]; ok {
				args = cached
				delete(s.pendingArgs, id)
			}
		}
	}
	depth := ReadEventDepth(data)
	persona := ReadEventPersona(data)
	preview := FormatToolPreview(chatAgent, name, args, s.VerboseMaxArgLen())

	// Compact mode: suppress result lines for successful tools.
	// Errors are always shown so the user sees what went wrong.
	// CLI-UX-3 exception: file edits still show a compact +N -M diffstat
	// so the user has minimal feedback in compact mode.
	if s.IsCompact() && status == "completed" {
		if diffSuffix := ComputeDiffStat(name, args); diffSuffix != "" {
			s.flushExternalWrite()
			fmt.Fprintf(os.Stderr, "%s%s%s\n", console.Esc(console.ColorDim), FormatCompactDiffLine(name, args, diffSuffix), console.Esc(console.ColorReset))
		}
		s.run = nil // prevent stale state from contaminating error tool collapse
		footer.Refresh()
		return
	}

	// Verbose mode: append a dim result-size suffix (e.g. "· 1.2KB")
	// to the tool-end line when the ToolEnd event carries result data.
	// Computed once here and spliced into both the collapse and fresh
	// line paths below.
	resultSuffix := ""
	if s.IsVerbose() {
		if resultLen := ReadEventInt(data, "result_length"); resultLen > 0 {
			if sizeStr := FormatResultSize(resultLen); sizeStr != "" {
				resultSuffix = fmt.Sprintf(" %s· %s%s", console.Esc(console.ColorDim), sizeStr, console.Esc(console.ColorReset))
			}
		}
	}

	// CLI-UX-3: For file-editing tools, append a compact +N -M diffstat
	// suffix so the user sees the change size at a glance. In compact
	// mode this is the ONLY feedback for file edits (full diff and
	// tool-end lines are suppressed). In default/verbose modes it
	// complements the existing tool-end line.
	if diffSuffix := ComputeDiffStat(name, args); diffSuffix != "" {
		resultSuffix += " " + diffSuffix
	}

	// Phase 3 collapse: if this end matches the prior run
	// (same name/depth/persona) AND less than 30s elapsed,
	// merge with the prior tool-end row instead of stacking
	// a new one. The 30s heuristic prevents collapse when
	// the model has streamed text between calls (which
	// would invalidate the row math).
	s.flushExternalWrite()
	now := time.Now()
	if s.run != nil && s.run.Matches(name, depth, persona) && now.Sub(s.run.LastEnd) < 30*time.Second {
		s.run.Count++
		s.run.AppendArg(preview)
		s.run.TotalMs += durationMs
		s.run.LastEnd = now
		s.run.LastIcon = icon
		// 2 rows up: the spinner row (now cleared by
		// Stop) + the blank stdout newline emitted by
		// ToolStart + the previous tool-end row. The
		// indicator's Stop already cleared the spinner
		// row in place, so we walk past the blank line
		// and the previous end-line.
		indicator.ReplaceLastN(FormatToolRunLine(
			s.run.Depth, s.run.Persona, s.run.LastIcon, s.run.Name,
			s.run.Count, s.run.ArgsTrail,
			float64(s.run.TotalMs)/1000.0,
		)+resultSuffix, s.rowsAboveSpinnerToPreviousEnd())
	} else {
		indicator.Replace(FormatToolEndLine(depth, persona, icon, name,
			preview, float64(durationMs)/1000.0) + resultSuffix)
		s.run = &ToolRunState{
			Name:     name,
			Depth:    depth,
			Persona:  persona,
			Count:    1,
			TotalMs:  durationMs,
			LastIcon: icon,
			LastEnd:  now,
		}
		s.run.AppendArg(preview)
	}
	footer.Refresh()

	// Display edit diff for file-editing tools in default/verbose mode.
	// Compact mode already showed just the diffstat and returned early.
	if status == "completed" && args != "" {
		s.MaybeDisplayEditDiff(name, args)
	}
	if len(s.toolsInFlight) > 0 {
		s.resumeToolSpinner(indicator)
	}
}
