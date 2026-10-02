//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// runInteractiveMode handles interactive REPL mode
func runInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) error {
	// SP-048 follow-up: Go's default logger writes to stderr — and so does
	// the activity-indicator spinner. Without redirection, any log.Printf
	// fired during a tool run (e.g. the [WARN] in pkg/configuration/config.go
	// when an AllowedTools override is dropped) interleaves with spinner
	// frames and produces the cursor-thrash bug we caught in real sessions.
	// Route Go's log to .sprout/workspace.log instead so internal noise
	// stops fighting the spinner; user-facing output still goes through
	// fmt.Print which is properly synchronized by the indicator.
	if restoreLog, err := redirectGoLogToWorkspace(); err == nil {
		defer restoreLog()
	}

	// SP-048-3: Persistent status footer pinned at the bottom row of the
	// terminal. Suppressed automatically on non-TTY (e.g., piped output).
	// MUST be Stopped before exit or the user's terminal is left with a
	// broken scroll region — both the defer here AND the signal handler's
	// force-quit path call Stop via the global registration.
	//
	// Started BEFORE the welcome/recent-sessions prints so that intro
	// output lands inside the scroll region (1..N-2) and scrolls naturally
	// as the session grows. Reverse order (prints first, then footer)
	// leaves the cursor inside already-printed content at row N-2, and
	// the input prompt then renders on top of it.
	footerSource := &agentFooterSource{agent: chatAgent}
	footer := console.NewStatusFooter(os.Stderr, footerSource)
	console.RegisterGlobalStatusFooter(footer)
	footer.Start()
	defer footer.Stop()

	// CLI-UX-12: register Alt+T (footer tooltip toggle) and Alt+V
	// (output verbosity toggle) in the global keymap so power users
	// can switch verbosity live without /settings + restart. The
	// verbosity toggle reads cfg.OutputVerbosity on each press; the
	// terminal subscriber's isVerbose()/isCompact() helpers pick up
	// the change on the next tool event (live-read). Idempotent — the
	// registry uses sync.Once, so multiple mode-bootstrap calls
	// (interactive + queue) only register once.
	console.RegisterKeymapForFooter(footer, chatAgent.GetConfigManager())

	// Compact startup chrome: a single greeting line with the active
	// provider/model so the first impression is "who am I talking to"
	// rather than four rows of welcome / provider / model / blank.
	// The previous form printed "Welcome to sprout! Enhanced CLI with
	// Web UI" + a second "Provider: X | Model: Y" line with trailing
	// blanks — five rows before the user could type anything.
	fmt.Println()
	console.GlyphInfo.Printf("sprout · %s · %s",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// SP-130: warn when the interactive session is running from the home
	// directory. The agent's workspace root is home, so every path under ~
	// is PathTierWorkspace (no approval prompts). This is the CLI analog of
	// the webui home-workspace gate — a warning, not a hard block, since
	// the user explicitly started sprout from ~.
	if cwd := chatAgent.GetWorkspaceRoot(); cwd != "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if abs, err := filepath.Abs(cwd); err == nil {
				if evaled, err := filepath.EvalSymlinks(abs); err == nil {
					if homeEvaled, err := filepath.EvalSymlinks(home); err == nil && evaled == homeEvaled {
						console.GlyphWarning.Printf(
							"Running in your home directory (%s) — the agent has access to all files under ~. Consider cd-ing into a project first.",
							home)
					}
				}
			}
		}
	}

	// SP-048-5a: surface recent sessions (last 7d) with inline numeric
	// selection. Up/down arrows stay reserved for command history; a
	// fresh number on its own line is the affordance. If the user picks
	// a session, this loads its state in-place via LoadStateScoped +
	// ApplyState + SetSessionID — same mechanism as `--session-id`,
	// just triggered interactively.
	//
	// Runs BEFORE the InputReader is constructed so a resumed session's
	// model is reflected in the prompt prefix. The returned dismissKey
	// is the first character the user typed to dismiss the picker (if
	// any) — it's forwarded into the input buffer below so that
	// keystroke isn't swallowed.
	dismissKey := maybeOfferSessionResume(chatAgent)

	// SP-048-5b: one-shot hint about Tab autocomplete + Ctrl-D, persisted
	// per workspace in ~/.sprout/state.json so it never repeats.
	maybeShowFirstRunHint()

	// Passive "new release available" check: background fetch on a 24h
	// throttle, stderr notice from the cache at most once per day. Silent
	// on every failure path.
	maybeStartUpdateCheck(chatAgent)
	maybeRenderUpdateNotice(chatAgent)

	// Create enhanced input reader with completion support.
	// SP-048-5d: prompt includes the current model so users always know
	// what they're talking to. Falls back to "sprout> " when the model
	// name is empty (e.g. provider failed to resolve at startup).
	inputReader := console.NewInputReader(cliui.BuildPromptPrefix(chatAgent.GetModel()))

	// Initialize with existing history from agent
	inputReader.SetHistory(chatAgent.GetHistory())

	// Forward the picker's dismiss key into the REPL input buffer so the
	// first character the user typed to start fresh isn't swallowed by
	// the session picker. SetInitialContent pre-fills the buffer; the
	// next ReadLine renders it with the cursor at the end.
	if dismissKey != "" {
		inputReader.SetInitialContent(dismissKey)
	}

	// SP-048-2a: slash command tab completion. The registry is cached
	// per-session (see slashCommandCache); argument completions are
	// TTL-cached to avoid network/config reads on every keystroke.
	completer := buildSlashCommandCompleter(chatAgent, false)
	inputReader.SetCompleter(completer)
	inputReader.SetRichCompleter(buildRichSlashCommandCompleter(chatAgent, false))

	// SP-078 Phase 4: the idle REPL dropdown renders above the prompt
	// line as a pinned block in the status footer's reserved rows
	// (steer-panel style), instead of the fragile below-line overlay.
	inputReader.SetStatusFooter(footer)

	// SP-055: steer coordinator owns the pinned steer-input panel for
	// the lifetime of this REPL. Constructed once with the agent +
	// footer references; StartTurn / EndTurn drive the per-iteration
	// lifecycle below.
	steerCoord := NewSteerCoordinator(chatAgent, footer)

	// SP-078 Phase 2: same slash-command completer on the steer panel
	// so Ctrl-] cycles slash commands mid-turn (Tab is reserved for
	// STEER ↔ QUEUE mode toggle on the steer panel).
	steerCoord.SetCompleter(buildSlashCommandCompleter(chatAgent, true))

	// SP-078 Phase 3: structured completer on the steer panel renders
	// a live dropdown above the input line while the user types a
	// "/" command (matches the InputReader's affordance). Tab accepts
	// the highlighted candidate; Up/Down navigate while the dropdown
	// is visible; Esc dismisses.
	steerCoord.SetRichCompleter(buildRichSlashCommandCompleter(chatAgent, true))

	// Capture a ground-truth termios snapshot of stdin in its default
	// cooked state (the terminal is fully cooked at this point — no
	// raw or steer mode active). Both InputReader and SteerInputReader
	// use this for emergency recovery: if a prior mode transition leaves
	// the terminal in raw mode, the pre-flight check restores to this
	// known-good state instead of a potentially-corrupted per-enter
	// snapshot. Must be captured AFTER footer.Start() so the scroll
	// region is established, but BEFORE any ReadLine / StartTurn call.
	groundTruth := console.CaptureGroundTruth()
	inputReader.SetGroundTruth(groundTruth)
	steerCoord.SetGroundTruth(groundTruth)

	// SP-048-1c + 3: Subscribe to tool start/end events so the activity
	// indicator can render a per-tool timeline AND the footer can refresh
	// cost/context after each tool. Runs until ctx is cancelled.
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	resetSpawnTracking := cliui.StartTerminalToolSubscriber(subCtx, chatAgent, eventBus, indicator, footer)
	cliui.SetREPLOwnsTurnSummary(true)
	defer cliui.SetREPLOwnsTurnSummary(false)

	// SP-108: Start a wakeup poller for CLI mode. This mirrors the WebUI
	// poller (pkg/webui/wakeup_poller.go), checking for pending background-
	// task completion notifications every 3s and auto-resuming the agent
	// so it can act on them. Without this, background tasks launched via
	// shell_command(background=true) complete silently in CLI mode —
	// notification is queued but nothing drains it until the user types
	// another message.
	//
	// The wake function routes TryAutoResume through the REPL loop (see
	// notifications.go): the poller stashes the wakeup batch and wakes the
	// idle ReadLine, and the loop runs the resume turn through its normal
	// turn machinery. This covers both the CLI poller and the shared-agent
	// WebUI poller, which share the same TryAutoResume entry point.
	//
	// ArmWakeup enables the readability-gated read loop so Wake() can
	// interrupt an idle prompt — without it, an already-parked Read only
	// sees the flag at its next iteration (i.e. on the next keystroke).
	inputReader.ArmWakeup()
	chatAgent.SetWakeupWakeFn(inputReader.Wake)
	go startCLIWakeupPoller(subCtx, chatAgent, indicator, 3*time.Second)

	return runInteractiveREPL(ctx, chatAgent, eventBus, indicator, interactiveREPLState{
		inputReader:        inputReader,
		steerCoord:         steerCoord,
		footer:             footer,
		resetSpawnTracking: resetSpawnTracking,
	})
}

// agentFooterSource adapts *agent.Agent to the console.ContentSource
// interface, exposing model / context tokens / cost / cwd to the status
// footer renderer.
type agentFooterSource struct {
	agent *agent.Agent
}

func (s *agentFooterSource) Model() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return cliui.ShortModelName(s.agent.GetModel())
}

func (s *agentFooterSource) ContextTokens() (used, limit int) {
	if s == nil || s.agent == nil {
		return 0, 0
	}
	return s.agent.GetContextTokens()
}

func (s *agentFooterSource) TotalCost() float64 {
	if s == nil || s.agent == nil {
		return 0
	}
	return s.agent.GetTotalCost()
}

// BillingType returns the current provider's billing model so the footer
// can annotate subscription/free usage instead of showing "$0.0000".
// Satisfies the optional billingTypeSource interface in pkg/console.
// SP-113 Phase 3.
func (s *agentFooterSource) BillingType() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return s.agent.ResolveBillingType()
}

// TodoProgress returns (completed, total) from the agent's todo list.
// Satisfies the optional todoProgressSource interface so the footer can
// render a "3/7 done" badge during multi-step turns. CLI-UX-4.
func (s *agentFooterSource) TodoProgress() (done, total int) {
	if s == nil || s.agent == nil {
		return 0, 0
	}
	todos := s.agent.GetTodoManager().Read()
	for _, t := range todos {
		total++
		if t.Status == "completed" {
			done++
		}
	}
	return done, total
}

func (s *agentFooterSource) WorkingDir() string {
	wd, _ := os.Getwd()
	return wd
}

// ActiveSubagents satisfies the optional activeSubagentsSource interface in
// pkg/console so the footer can render " · N sub" while subagents are
// in flight. SP-051-2d.
func (s *agentFooterSource) ActiveSubagents() int {
	return agent.GetActiveSubagents()
}

// QueuedMessages satisfies the optional queuedMessagesSource interface
// so the footer renders a "⏸ N queued" badge when the user has
// deferred steer messages via Tab+Enter waiting for the next turn.
// SP-055 Phase 3b.
func (s *agentFooterSource) QueuedMessages() int {
	if s.agent == nil {
		return 0
	}
	return s.agent.DeferredMessageCount()
}
