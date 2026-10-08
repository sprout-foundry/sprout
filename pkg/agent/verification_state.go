// verification_state.go — the accessor for the turn-end hook's stored
// per-turn verification state for
// consumers outside pkg/agent: the benchmark per-task metrics
// and the verification event. The hook stores a fresh
// snapshot on every verification run (pass, fail, stop-rule);
// prepareQueryRun resets the state at each turn start, so a previous
// turn's result never attaches to a later turn's reply.

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// markTurnVerificationSetupError records that this turn's verification
// run hit a runner setup error and stored nothing. Guarded by
// turnVerificationMu (the per-agent lock over the per-turn verification
// state), so consumers reading it through NotVerifiedReason never race
// the hook that writes it.
func (a *Agent) markTurnVerificationSetupError() {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	a.turnVerificationSetupErr = true
}

// NotVerifiedReason states why the agent's current turn has no
// verification result (Agent.LastVerificationResult == nil), reusing the
// same reasons the turn-completion event carries
// (progress_complete.not_verified_reason). It is the accessor consumers
// outside pkg/agent use — the benchmark report records it on a run whose
// verification never produced a result, so a failed run always carries
// the reason instead of reading as a silent failure.
//
// The values are:
//
//   - "" — verification is disabled for this agent (no configuration
//     manager, or the configuration does not enable verification);
//   - "no code changes this turn" — verification is enabled but the turn
//     changed no application code (the hook's change gate);
//   - "verification setup error" — the turn changed application code and
//     the hook entered, but its verify runner hit a setup error and
//     stored nothing;
//   - "verification did not run this turn" — verification is enabled and
//     code changed, but the hook still did not run (a subagent turn, or
//     the reason was not otherwise recorded).
func (a *Agent) NotVerifiedReason() string {
	if a == nil {
		return ""
	}
	cfgEnabled := a.configManager != nil && a.configManager.GetConfig() != nil &&
		a.configManager.GetConfig().VerificationEnabled()
	a.turnVerificationMu.Lock()
	setupErr := a.turnVerificationSetupErr
	a.turnVerificationMu.Unlock()
	return notVerifiedReasonFor(cfgEnabled, len(a.TurnChangedApplicationPaths()) > 0, setupErr)
}

// notVerifiedReasonFor is the pure reason decision shared by the event
// builder (notVerifiedReason) and the exported accessor
// (NotVerifiedReason): the same string the turn's progress_complete
// event carries is the string the benchmark report records.
func notVerifiedReasonFor(verificationEnabled, codeChanged, setupError bool) string {
	if !verificationEnabled {
		return ""
	}
	if setupError {
		return "verification setup error"
	}
	if !codeChanged {
		return "no code changes this turn"
	}
	return "verification did not run this turn"
}

// TurnVerification is one turn's end-verification state
// for consumers outside pkg/agent (the benchmark
// metrics, and the verification event later): the run's
// result, the per-check repair attempts consumed against the
// stopping rule, the configured limit, and how many repair rounds
// the hook ran.
type TurnVerification struct {
	// Result is the turn's last verification run (the hook stores a
	// state on every run, pass, fail, or stop-rule; a non-nil
	// LastTurnVerification always carries a non-nil Result).
	Result *verify.Result
	// Attempts is the per-check repair attempts the hook consumed
	// against the stopping rule before its last verification run,
	// keyed like the hook's per-check counters (checkAttemptKey). It
	// is a copy: mutating it does not affect the agent's stored
	// state. Empty (nil) when the last run saw no repair rounds yet
	// (a passing run, or the first run of a failing sequence).
	Attempts map[string]int
	// Limit is the stopping-rule limit that was in effect for the
	// turn (stored on every run, pass or fail).
	Limit int
	// Rounds is how many repair rounds the hook ran within the turn
	// (0 for a turn that verified once and passed; a turn where the
	// hook never ran reports LastTurnVerification == nil, not
	// Rounds == 0).
	Rounds int
}

// LastTurnVerification returns the turn's stored end-verification state
// (stored by the turn-end hook on every
// verification run), or nil when the hook never ran for the turn
// (verification disabled, the turn changed no code, a subagent turn,
// or a runner setup error) — including on a nil receiver. Attempts is
// a copy: mutating it never affects the agent's stored state.
func (a *Agent) LastTurnVerification() *TurnVerification {
	if a == nil {
		return nil
	}
	stored := a.lastTurnVerificationSnapshot()
	if stored.result == nil {
		return nil
	}
	return &TurnVerification{
		Result:   stored.result,
		Attempts: stored.attempts,
		Limit:    stored.limit,
		Rounds:   stored.rounds,
	}
}
