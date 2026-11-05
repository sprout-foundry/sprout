//go:build !js

package cliui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// turn_outcome.go — the plain-language outcome line every finished LLM
// turn prints before the closing marker. Where the marker (turn_stats.go)
// says "the turn is over", this line says "and here is how it went":
// succeeded, failed, or stopped, in the user's words rather than a raw
// exception. Detail is never invented: the failure phrasing is a fixed
// mapping from the runtime's own error classification
// (pkg/errors TypedError codes, the legacy AgentError categories), and
// when an SP-149 verification result exists its summary is used verbatim
// instead of any restatement.

// TurnOutcome states how a finished turn ended.
type TurnOutcome int

const (
	// TurnSucceeded means the turn ran to a normal end.
	TurnSucceeded TurnOutcome = iota
	// TurnFailed means the turn gave up: a classified error, or a
	// failed verification result.
	TurnFailed
	// TurnStopped means the turn ended partway: interrupted, or an
	// iteration/budget limit fired.
	TurnStopped
)

// turnOutcomeMaxChangedFiles caps how many file paths the success line
// lists before it collapses to a count.
const turnOutcomeMaxChangedFiles = 3

// TurnOutcomeFacts carries what the runtime observed about a finished
// turn. Every field is optional; the renderer says only what the facts
// support.
type TurnOutcomeFacts struct {
	// Err is the error the turn ended with, if any. Classified via
	// pkg/errors to pick the failure phrasing.
	Err error
	// TerminationReason is the agent's last run termination reason
	// (agent.RunTermination* constants). Interrupt/limit reasons make
	// the turn "stopped" even when an interrupt-shaped error is also
	// present — a stop is not a failure.
	TerminationReason string
	// Verification is the turn's SP-149 verification result, when the
	// turn-end hook ran one. Its Summary() is used verbatim.
	Verification *verify.Result
	// ChangedPaths are the workspace paths this turn changed, when
	// change tracking observed any.
	ChangedPaths []string
	// Duration is the turn's wall time.
	Duration time.Duration
	// MaxIterations is the configured per-turn iteration cap, used only
	// to phrase the max-iterations stop reason.
	MaxIterations int
}

// ResolveTurnOutcome classifies a finished turn into succeeded / failed /
// stopped. The termination reason wins over the error: an interrupted
// turn returns an interrupt-shaped error and must read as stopped, not
// failed.
func ResolveTurnOutcome(facts TurnOutcomeFacts) TurnOutcome {
	switch facts.TerminationReason {
	case agent.RunTerminationInterrupted, agent.RunTerminationMaxIterations, agent.RunTerminationFleetBudgetExceeded:
		return TurnStopped
	}
	if facts.Err != nil {
		return TurnFailed
	}
	if facts.Verification != nil && facts.Verification.Failed() {
		return TurnFailed
	}
	return TurnSucceeded
}

// FormatTurnOutcomeLine renders the one-line outcome sentence for a
// finished turn, with its leading glyph and color already applied. The
// line is dim by default — it states the outcome without shouting; the
// error block above it already carries the raw evidence.
func FormatTurnOutcomeLine(facts TurnOutcomeFacts) string {
	switch ResolveTurnOutcome(facts) {
	case TurnFailed:
		return formatFailedOutcome(facts)
	case TurnStopped:
		return formatStoppedOutcome(facts)
	default:
		return formatSucceededOutcome(facts)
	}
}

// PrintTurnOutcome writes the outcome line to stderr. Silent when stderr
// is not a TTY, mirroring the turn summary's gate. Test-only capture goes
// through the same sink the turn marker uses.
func PrintTurnOutcome(facts TurnOutcomeFacts) {
	writeTurnEnd(FormatTurnOutcomeLine(facts))
}

// formatSucceededOutcome states what a completed turn did: the
// verification result verbatim when one exists, else the deterministic
// turn facts (changed files, wall time). The model's own reply is already
// on screen and is never restated here.
func formatSucceededOutcome(facts TurnOutcomeFacts) string {
	if facts.Verification != nil {
		if facts.Verification.Passed() {
			return console.GlyphSuccess.Prefix() + console.SGR(os.Stderr, console.ColorDim) +
				"Turn succeeded — " + facts.Verification.Summary() + console.SGR(os.Stderr, console.ColorReset)
		}
		// A result that neither passed nor failed ran no checks; say so
		// rather than implying anything was verified.
		return console.GlyphSuccess.Prefix() + console.SGR(os.Stderr, console.ColorDim) +
			"Turn succeeded (nothing was verified)" + console.SGR(os.Stderr, console.ColorReset)
	}
	parts := changedFilesSegment(facts.ChangedPaths)
	if facts.Duration > 0 {
		parts = append(parts, CompactDuration(facts.Duration))
	}
	line := "Turn succeeded"
	if len(parts) > 0 {
		line += " · " + strings.Join(parts, " · ")
	}
	return console.GlyphSuccess.Prefix() + console.SGR(os.Stderr, console.ColorDim) + line + console.SGR(os.Stderr, console.ColorReset)
}

// formatFailedOutcome names the cause in the user's words. The phrasing
// comes from a fixed mapping over the runtime's error taxonomy — the raw
// exception never reaches this line.
func formatFailedOutcome(facts TurnOutcomeFacts) string {
	cause := ""
	if facts.Verification != nil && facts.Verification.Failed() && facts.Err == nil {
		cause = "verification failed — " + facts.Verification.Summary()
	} else {
		cause = failureCause(facts.Err)
	}
	line := "Turn failed"
	if cause != "" {
		line += " — " + cause
	}
	return console.GlyphError.Prefix() + console.SGR(os.Stderr, console.ColorDim) + line + console.SGR(os.Stderr, console.ColorReset)
}

// formatStoppedOutcome says what completed and that the turn ended before
// finishing on its own.
func formatStoppedOutcome(facts TurnOutcomeFacts) string {
	var reason string
	switch facts.TerminationReason {
	case agent.RunTerminationMaxIterations:
		reason = fmt.Sprintf("reached the iteration limit (%d)", facts.MaxIterations)
	case agent.RunTerminationFleetBudgetExceeded:
		reason = "the fleet token budget ran out"
	default:
		reason = "interrupted"
	}
	line := "Turn stopped — " + reason
	if facts.Duration > 0 {
		line += " after " + CompactDuration(facts.Duration)
	}
	return console.GlyphStopped.Prefix() + console.SGR(os.Stderr, console.ColorDim) + line + console.SGR(os.Stderr, console.ColorReset)
}

// changedFilesSegment turns the turn's changed paths into the success
// line's file segment: up to three paths by name, else a bare count.
func changedFilesSegment(paths []string) []string {
	var parts []string
	if len(paths) == 0 {
		return parts
	}
	if len(paths) <= turnOutcomeMaxChangedFiles {
		names := make([]string, 0, len(paths))
		for _, p := range paths {
			names = append(names, shortPathName(p))
		}
		parts = append(parts, "changed "+strings.Join(names, ", "))
		return parts
	}
	parts = append(parts, fmt.Sprintf("changed %d files", len(paths)))
	return parts
}

// shortPathName trims a tracked path to its workspace-relative tail so a
// success line stays one line.
func shortPathName(p string) string {
	p = strings.TrimPrefix(p, "./")
	if i := strings.LastIndex(p, "/"); i >= 0 && i < len(p)-1 {
		if len(p)-i-1 <= 0 {
			return p
		}
		// Keep the last two components: file plus its immediate directory.
		if j := strings.LastIndex(p[:i], "/"); j >= 0 {
			return p[j+1:]
		}
	}
	return p
}

// failureCause maps a classified error to a plain sentence. Unknown errors
// yield "" — the error block above the outcome line already shows them, and
// inventing a cause here would be worse than silence.
func failureCause(err error) string {
	if err == nil {
		return ""
	}
	if te := agenterrors.AsTypedError(err); te != nil {
		if msg := typedErrorCodeCause(te.Code); msg != "" {
			return msg
		}
	}
	if cat, ok := agenterrors.GetCategory(err); ok {
		if msg := categoryCause(err, cat); msg != "" {
			return msg
		}
	}
	if errors.Is(err, agent.ErrRunInterrupted) {
		return "interrupted"
	}
	return ""
}

// typedErrorCodeCause phrases SP-094 typed-error codes. Returned in the
// order the switch reads; "" falls through to the legacy taxonomy.
func typedErrorCodeCause(code agenterrors.ErrorCode) string {
	switch code {
	case agenterrors.CodeNetwork:
		return "network difficulty with the model — gave up"
	case agenterrors.CodeTimeout:
		return "the model took too long to respond — gave up"
	case agenterrors.CodePermission:
		return "a permission rule blocked the work"
	case agenterrors.CodeApproval:
		return "an approval gate blocked the work"
	case agenterrors.CodeValidation:
		return "the request was rejected as invalid"
	case agenterrors.CodeNotFound:
		return "something the turn needed was not found"
	case agenterrors.CodeConfig:
		return "a configuration problem stopped the turn"
	case agenterrors.CodeTool:
		return "a tool call failed and the turn could not continue"
	case agenterrors.CodeAgent:
		return "the agent hit an internal error"
	default:
		return ""
	}
}

// categoryCause phrases the legacy AgentError categories. Provider errors
// get a sub-classification from their cause because auth, model-not-found,
// and overload read differently to a user.
func categoryCause(err error, cat agenterrors.ErrorCategory) string {
	switch cat {
	case agenterrors.CategoryTransient:
		return "network difficulty with the model — gave up"
	case agenterrors.CategoryRateLimited:
		return "the provider rate-limited the model — gave up"
	case agenterrors.CategoryContext:
		return "the conversation outgrew the model's context window"
	case agenterrors.CategorySecurity:
		return "a security rule blocked the work"
	case agenterrors.CategoryInvalidInput:
		return "the request was rejected as invalid"
	case agenterrors.CategoryProvider:
		return providerCause(err)
	case agenterrors.CategoryPermanent:
		return "the turn hit a permanent error"
	default:
		return ""
	}
}

// providerCause splits provider errors into the three shapes a user can
// act on: credentials, model availability, and provider overload.
func providerCause(err error) string {
	cause := ""
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		cause = strings.ToLower(unwrapped.Error())
	} else {
		cause = strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(cause, "unauthorized"),
		strings.Contains(cause, "401"),
		strings.Contains(cause, "authentication"),
		strings.Contains(cause, "invalid api key"):
		return "the provider rejected the credentials"
	case strings.Contains(cause, "model not found"),
		strings.Contains(cause, "model not exist"),
		strings.Contains(cause, "invalid model"):
		return "the configured model is not available on the provider"
	case strings.Contains(cause, "502"),
		strings.Contains(cause, "503"),
		strings.Contains(cause, "overloaded"),
		strings.Contains(cause, "internal server error"):
		return "the provider is overloaded — gave up"
	default:
		return "the model provider request failed"
	}
}
